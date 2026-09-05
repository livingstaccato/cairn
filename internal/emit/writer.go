// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package emit writes cairn's output formats. Every write goes through Writer,
// which enforces path containment, the protect globs and the conflict policy.
// That single choke point is what keeps cairn from ever clobbering apt or yum
// repository metadata, or writing outside the directory it was given.
package emit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livingstaccato/cairn/internal/atomicfile"
	"github.com/livingstaccato/cairn/internal/config"
)

// ManifestFile records the paths cairn generated, so a later run can tell its
// own output apart from content that was already there.
const ManifestFile = ".cairn-manifest.json"

// manifestVersion is stamped into every manifest so a later shape can be told
// apart from this one without guessing.
const manifestVersion = 1

// manifest is what cairn owns under one output root, and what it put there.
//
// The digest is why the shape changed. A path alone answers "may cairn replace
// this", which is all a build needs; it cannot answer "does this still hold what
// cairn wrote", which is what an operator asks of a mirror they are unsure
// about. Generated output appears in no SHA256SUMS — a listing excludes cairn's
// own files — so without this nothing records it at all.
type manifest struct {
	Version int               `json:"version"`
	Outputs map[string]string `json:"outputs"`
}

// ParseManifest reads a manifest and returns each claimed path with the digest
// recorded for it.
func ParseManifest(b []byte) (map[string]string, error) { return parseManifest(b) }

func parseManifest(b []byte) (map[string]string, error) {
	// Decoded through a pointer so an absent "outputs" is distinguishable from
	// an empty one. A JSON object that is not a manifest would otherwise parse
	// as cairn claiming nothing, and every file in the tree would then be
	// reported as output cairn does not own — a lie about the tree rather than
	// a finding about it.
	var m struct {
		Version int                `json:"version"`
		Outputs *map[string]string `json:"outputs"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Outputs == nil {
		return nil, fmt.Errorf("no outputs recorded; not a cairn manifest")
	}
	for claim, sum := range *m.Outputs {
		if !isHex64(sum) {
			return nil, fmt.Errorf("output %s carries no usable digest", claim)
		}
	}
	return *m.Outputs, nil
}

// digestOf is the SHA-256 of one output body, hex encoded — the same form
// SHA256SUMS uses, so a person comparing the two by eye is comparing like
// with like.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Output permissions. These are deliberately looser than gosec's defaults:
// cairn writes a static site that a web server reads as a different user, so
// 0600 files and 0750 directories would produce a tree nginx cannot serve. The
// content is public by construction — it is a published index.
//
// DirMode is exported because it is a decision about the shape of the output
// tree rather than a detail of writing one file: anything that creates a
// directory cairn will later fill — `cairn watch --serve`, which opens the
// socket before the first build has made the tree — has to create it the same
// way, or the served directory is one the web server cannot enter.
const (
	DirMode     os.FileMode = 0o755
	outFileMode os.FileMode = 0o644
)

// Writer places output files under one root.
//
// It carries a manifest because a generator that cannot be run twice is not
// finished. Without one, the second build sees the first build's index.json and
// reports a conflict; with one, cairn overwrites what it previously wrote and
// still refuses to touch anything it did not.
type Writer struct {
	cfg   *config.Config
	root  string
	own   map[string]string // paths this run may overwrite, and what they held
	made  []string          // paths written by this run
	prot  []string          // paths a protect: glob kept this run from writing
	wrote []string          // paths whose bytes this run actually changed
	sums  map[string]string // what this run put at each path
	took  []string          // paths this run claimed that it did not previously own
	dry   bool              // report what a build would do, change nothing
	adopt bool              // claim an existing path cairn does not own
	mErr  error             // why the previous manifest was not usable, if it was not
}

// Options are the departures from default behaviour a single run may ask for.
//
// Neither is expressible in cairn.yaml, on purpose. A config that permanently
// suppressed the conflict check would turn off the property that stops cairn
// overwriting somebody else's files — silently, on every run after the one that
// needed it. These are asked for on the command line, for one run, by somebody
// who is watching.
type Options struct {
	// Dry answers every question a build asks and changes nothing under the
	// output root.
	Dry bool
	// Adopt claims an output path that already exists and cairn does not own,
	// instead of refusing it.
	Adopt bool
}

// NewWriter loads the previous run's manifest, if any. A missing or unreadable
// manifest yields an empty one: the cost is a conflict error the operator can
// resolve, never a silent overwrite.
func NewWriter(cfg *config.Config, outRoot string) *Writer {
	return NewWriterWith(cfg, outRoot, Options{})
}

// NewWriterWith is NewWriter with the one-run departures a command line asked
// for.
func NewWriterWith(cfg *config.Config, outRoot string, opts Options) *Writer {
	w := &Writer{
		cfg: cfg, root: outRoot, own: map[string]string{}, sums: map[string]string{},
		dry: opts.Dry, adopt: opts.Adopt,
	}
	// #nosec G304 -- composed from the configured output directory and a fixed
	// filename.
	b, err := os.ReadFile(filepath.Join(outRoot, ManifestFile))
	if err != nil {
		return w
	}
	own, err := parseManifest(b)
	if err != nil {
		// Kept rather than returned, so the caller can say why. A manifest that
		// will not parse means cairn claims nothing, and the next thing that
		// happens is on_conflict: error refusing a file it wrote itself last
		// week — with a message about a path that already exists, which sends
		// an operator looking in exactly the wrong place.
		w.mErr = err
		return w
	}
	w.own = own
	return w
}

// ManifestError reports why the previous run's manifest was unusable, or nil
// when there was none to read or it read cleanly. Losing a manifest is not
// itself fatal — cairn simply owns nothing — but it changes what every later
// error in the run means, so a caller states it up front.
func (w *Writer) ManifestError() error { return w.mErr }

// NewDryWriter loads the same manifest and answers the same questions, but
// creates, replaces and deletes nothing.
//
// Prune is the only irreversible thing cairn does, and until now the only way
// to learn what it would remove was to let it. Every decision still runs — the
// containment check, the protect globs, the conflict policy, the digest of each
// body against what is on disk — so Written, Changed and the pruned list say
// exactly what a real run would have done, rather than approximating it.
func NewDryWriter(cfg *config.Config, outRoot string) *Writer {
	return NewWriterWith(cfg, outRoot, Options{Dry: true})
}

// Write places body at root/relPath, creating parents.
//
// A protect: glob skips the write and reports no error. The glob is the operator
// declaring which paths another tool owns — apt's signed dists/, dnf's repodata/
// — so refusing to write there is exactly what was asked for, and failing the
// build over it would make protect: unusable for its only purpose. A protected
// path is never recorded as written: claiming it would let a later run overwrite
// the files protect: exists to shield, and Prune would delete them.
func (w *Writer) Write(relPath string, body []byte) error {
	if w.cfg.IsProtected(relPath) {
		w.prot = append(w.prot, relPath)
		return nil
	}

	abs, err := containedPath(w.root, relPath)
	if err != nil {
		return err
	}

	if err := w.checkConflict(relPath, abs); err != nil {
		return err
	}
	if w.skipped(relPath, abs) {
		return nil
	}
	// Recorded whether or not the bytes are written, because the manifest has to
	// say what stands at the path, not what this particular run happened to move.
	w.sums[relPath] = digestOf(body)

	if atomicfile.Same(abs, body) {
		// Recorded as written even though nothing was written. The manifest is
		// what stops the next run's Prune from deleting it, and a file skipped
		// here is still a file this run owns.
		w.made = append(w.made, relPath)
		return nil
	}

	if w.dry {
		// Counted as written and as changed: this is the run reporting what it
		// would have done, and a caller that filtered dry-run output down to
		// nothing would be told a build has nothing to do.
		w.made = append(w.made, relPath)
		w.wrote = append(w.wrote, relPath)
		return nil
	}

	// #nosec G301 -- see DirMode: the served tree must be traversable by the
	// web server's user.
	if err := os.MkdirAll(filepath.Dir(abs), DirMode); err != nil {
		return fmt.Errorf("create parents for %s: %w", relPath, err)
	}
	// #nosec G306 -- see outFileMode: a published index is world-readable by
	// design.
	if err := os.WriteFile(abs, body, outFileMode); err != nil {
		return fmt.Errorf("write %s: %w", relPath, err)
	}
	w.made = append(w.made, relPath)
	w.wrote = append(w.wrote, relPath)
	return nil
}

// checkConflict reports an error when something cairn does not own already
// occupies the path and the policy is to fail.
//
// Lstat, not Stat: a symlink sitting at the output path must count as a conflict
// rather than being followed and silently overwriting whatever it targets. Stat
// would also report a dangling link as absent and let the write through.
func (w *Writer) checkConflict(relPath, abs string) error {
	if !exists(abs) {
		return nil
	}
	if _, ours := w.own[relPath]; ours {
		return nil // cairn wrote it last run; overwriting is the whole point
	}
	if w.adopt {
		// Ahead of the skip policy on purpose: under skip the path would be
		// left alone and stay unowned, so the next run would skip it again and
		// Prune could never reach it. Asking to adopt is asking to take it.
		w.took = append(w.took, relPath)
		return nil
	}
	if w.cfg.OnConflict == config.ConflictSkip {
		return nil
	}
	return fmt.Errorf("refusing to write %s: path already exists and cairn did not create it "+
		"(set on_conflict: %s, or index_basename to something unused)",
		relPath, config.ConflictSkip)
}

// skipped reports whether an existing, unowned path should be left alone.
func (w *Writer) skipped(relPath, abs string) bool {
	if w.adopt {
		return false
	}
	_, ours := w.own[relPath]
	return exists(abs) && !ours && w.cfg.OnConflict == config.ConflictSkip
}

// exists reports whether anything occupies p — including a symlink, dangling or
// not. Lstat rather than Stat is the whole point: a link standing at an output
// path is something, and following it would overwrite its target.
func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Written returns the paths this run produced.
func (w *Writer) Written() []string { return w.made }

// Changed lists the paths whose bytes this run actually altered.
//
// Written is what cairn owns; this is what moved. A deployment that syncs a
// mirror does not need to re-upload a listing that is byte-identical to the one
// already published, and until now nothing could tell it which those were — the
// build knew and threw the answer away.
func (w *Writer) Changed() []string { return w.wrote }

// Unchanged counts the paths that already held what this run would have
// written. The caller reports it: on a rebuild it is the difference between
// "nothing needed doing" and "everything was rewritten identically", which is
// the number an operator watching a mirror wants.
func (w *Writer) Unchanged() int { return len(w.made) - len(w.wrote) }

// Adopted lists the paths this run claimed that no previous run had recorded.
//
// The set is exactly the paths this build produces that already existed, which
// is what makes --adopt safe to reason about: it never walks the output root
// guessing which files look generated, and it can claim nothing the build does
// not itself write. The caller reports it, because waiving the conflict check
// is the one thing an operator must not discover afterwards.
func (w *Writer) Adopted() []string { return w.took }

// Protected lists the paths this run declined to write because a protect: glob
// covered them. The caller reports the count: a skip is silent by design, and
// without it an operator whose glob is wider than they meant sees no listing and
// no reason why.
func (w *Writer) Protected() []string { return w.prot }

// Prune deletes output from the previous run that this run did not write, and
// returns the paths it removed.
//
// Without this a removed file leaves its digest behind and, worse, a removed
// directory leaves a whole published listing for something that is gone — links
// and all. A generator that only ever adds is not maintaining a mirror.
//
// Only paths the previous manifest recorded are considered, so cairn can delete
// nothing it did not create. A missing or corrupt manifest prunes nothing, which
// is the safe failure: stale files are a nuisance, deleting someone's artifacts
// is not.
//
// Two configs sharing one output root would each prune the other's files. That
// is unsupported rather than guarded against — there is no way to tell that case
// apart from a directory that was legitimately removed.
// underScope reports whether a manifest path belongs to a scoped rebuild.
//
// An empty scope is the whole tree. Comparison is on path segments so that
// "docs" does not claim "docs-old": a prefix test on the raw string would
// prune a sibling directory's entire output.
func underScope(p, scope string) bool {
	if scope == "" || scope == "." {
		return true
	}
	rel := strings.TrimPrefix(p, "/")
	return rel == scope || strings.HasPrefix(rel, scope+"/")
}

// PruneScoped removes stale output from one subtree, leaving the rest of the
// manifest's claims alone.
//
// A scoped rebuild writes only part of the tree, so the unscoped Prune — which
// deletes everything the previous run owned and this one did not rewrite —
// would delete every listing outside the scope on the first watch event.
func (w *Writer) PruneScoped(scope string) ([]string, error) {
	return w.prune(scope)
}

func (w *Writer) Prune() ([]string, error) { return w.prune("") }

func (w *Writer) prune(scope string) ([]string, error) {
	wrote := make(map[string]bool, len(w.made))
	for _, p := range w.made {
		wrote[p] = true
	}

	var removed []string
	for p := range w.own {
		if wrote[p] || !underScope(p, scope) {
			continue
		}
		abs, err := containedPath(w.root, p)
		if err != nil {
			return removed, err
		}
		if !w.dry {
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("prune %s: %w", p, err)
			}
		}
		removed = append(removed, p)
	}
	sort.Strings(removed)
	if !w.dry {
		w.pruneEmptyDirs(removed)
	}
	return removed, nil
}

// pruneEmptyDirs removes directories left holding nothing after a prune.
//
// Deepest first, so a nested pair collapses in one pass, and only when empty —
// os.Remove fails harmlessly on a directory that still holds an artifact, which
// is exactly the guard wanted here.
func (w *Writer) pruneEmptyDirs(removed []string) {
	dirs := make([]string, 0, len(removed))
	for _, p := range removed {
		if d := path.Dir(p); d != "." {
			dirs = append(dirs, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		abs, err := containedPath(w.root, d)
		if err != nil {
			continue
		}
		_ = os.Remove(abs)
	}
}

// Save records this run's output so the next run knows what it may replace.
func (w *Writer) Save() error { return w.save(w.made) }

// SaveScoped records a scoped rebuild: what it wrote, plus everything the
// previous run owned outside the scope. Saving only what a scoped run wrote
// would disown the rest of the tree, and the next run would refuse all of it
// as somebody else's files.
func (w *Writer) SaveScoped(scope string) error {
	seen := make(map[string]bool, len(w.made))
	all := make([]string, 0, len(w.own)+len(w.made))
	for _, p := range w.made {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	outside := make([]string, 0, len(w.own))
	for p := range w.own {
		if !seen[p] && !underScope(p, scope) {
			outside = append(outside, p)
		}
	}
	sort.Strings(outside) // map order would churn the manifest between runs
	return w.save(append(all, outside...))
}

// SavePartial records this run's output together with everything the previous
// run claimed, for a build that died partway.
//
// The union matters. Saving only what a failed run managed to write would
// disown every file an earlier successful run had created but this one never
// reached, and the next build would refuse all of them as conflicts — a worse
// wedge than saving nothing. Stale entries are not a problem: the next run that
// completes prunes whatever it does not rewrite.
func (w *Writer) SavePartial() error {
	all := make([]string, 0, len(w.own)+len(w.made))
	seen := make(map[string]bool, len(w.own)+len(w.made))
	for _, p := range w.made {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	prev := make([]string, 0, len(w.own))
	for p := range w.own {
		if !seen[p] {
			prev = append(prev, p)
		}
	}
	sort.Strings(prev) // map order would make the manifest churn between runs
	return w.save(append(all, prev...))
}

func (w *Writer) save(paths []string) error {
	m := manifest{Version: manifestVersion, Outputs: make(map[string]string, len(paths))}
	for _, p := range paths {
		// This run's digest where it has one, the previous run's for a path
		// carried forward by a scoped or partial save. A path with neither
		// stays claimed with an empty digest: ownership is not in doubt, only
		// what the file should hold.
		if sum, ok := w.sums[p]; ok {
			m.Outputs[p] = sum
			continue
		}
		m.Outputs[p] = w.own[p]
	}

	if w.dry {
		return nil
	}

	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	abs := filepath.Join(w.root, ManifestFile)
	// #nosec G301 -- see DirMode: the served tree must be traversable by the
	// web server's user.
	if err := os.MkdirAll(w.root, DirMode); err != nil {
		return fmt.Errorf("create output root: %w", err)
	}
	// Replaced rather than rewritten in place, and left alone when it already
	// says this. A manifest is the one file whose loss cannot be repaired by
	// running cairn again — without it the next build disowns everything the
	// last one wrote — and on a large mirror it is tens of megabytes that a
	// rebuild changing nothing has no reason to touch.
	if _, err := atomicfile.Write(abs, b, outFileMode); err != nil {
		return err
	}
	return nil
}

// containedPath joins relPath onto outRoot and verifies the result stays inside
// it. filepath.Join cleans "..", so a crafted relPath resolves quietly outside
// the root unless containment is checked explicitly.
//
// Only outRoot is symlink-resolved. The target usually does not exist yet —
// that is the point of writing it — so resolving it would fail for every new
// file. A symlink already standing at the target is caught by the conflict
// check instead, which refuses rather than following it.
func containedPath(outRoot, relPath string) (string, error) {
	rootAbs, err := filepath.Abs(outRoot)
	if err != nil {
		return "", fmt.Errorf("resolve output root %s: %w", outRoot, err)
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}

	abs := filepath.Join(rootAbs, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", relPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write %s: resolves outside the output root", relPath)
	}
	return abs, nil
}
