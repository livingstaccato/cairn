# cairn — working agreement

Static directory-index and artifact-repo generator. A Go binary and a Hugo
module in one repo, so emitted data and the templates that render it share a
single version pin.

This repo is public and general-purpose. Nothing identifying goes in it: no
employer names, no private repo names, no internal hostnames, no internal paths,
no personal addresses — in code, **fixtures**, docs, commit messages or tag
messages. Everything here ships in a Go module zip that cannot be unpublished.
`ci/check-private.sh` enforces it. The design docs are not in this repo; see the
local `CLAUDE.md`, which is gitignored.

## Before you start

```sh
make tools    # install golangci-lint, gosec, govulncheck
make gate     # gofmt, vet, golangci-lint, max-loc, gosec, govulncheck, tests
```

`make gate` must be green before every commit. Not "usually" — every commit.
The gate landed before the first line of logic on purpose: a gate added late
only tells you how much you already owe.

## Layout

```
cmd/cairn/       CLI entry point, thin — logic lives in internal/
internal/
  model/         Entry, Listing — the JSON/YAML contract
  config/        cairn.yaml, .cairn.yaml, rule matching, precedence
  walk/          producers: fs, pages, manifest + kind inference
  meta/          _meta.yaml, sidecars, directory prose
  hash/          SHA-256 with a (path,size,mtime) cache
  emit/          html, json, csv, sums, pep503 + the write guard
  build/         orchestration: walk → merge → hash → emit
  watch/         which directories to watch, what to ignore, what to rebuild
layouts/         Hugo component module (no baseof, no brand)
assets/cairn/    CSS, JS, SVG sprite — self-contained, no CDN
themes/reference/ minimal standalone theme, own go.mod
ci/              one script per gate; never inline YAML
exampleSite/     end-to-end proof
```

## File splitting

**Hard ceiling 777 lines per Go file**, enforced by `ci/check-max-loc.sh`.
Target 200–300. The ceiling is a backstop, not a budget to spend.

Split by **responsibility**, never by technical layer. `walk/kind.go` owns
"what kind of thing is this filename", `walk/fs.go` owns "what is in this
directory" — two files because they are two questions, not because one is
types and the other is functions.

Why it matters here specifically: a file you can hold in context at once is one
you can reason about correctly, and edits to a focused file are more reliable
than edits to a sprawling one. That applies to humans reading a diff and to
agents editing by pattern match. A file drifting past ~400 lines is usually
answering more than one question — split it before the ceiling forces you to.

One package doc comment, in the file the package is named after. Test file
beside its source: `fs.go` → `fs_test.go`.

## Non-negotiables

These come from the design and each has a test. Do not relax one to make a
test pass.

- **`Entry.Size` is exact bytes.** Formatting happens only in templates.
  Dividing by 1024 at the source renders every sub-kilobyte file as `0KB`.
- **Zero external runtime assets** in generated output. No CDN, no web fonts,
  no icon font. Icons are an inline SVG sprite. It has to work airgapped.
- **`bare` presenter emits no `<script>`** and renders in `lynx`.
- **`emit.Writer` is the only writer.** It checks path containment, `protect:`
  globs, and conflicts, and it records what cairn generated so a re-run may
  replace its own output and nothing else. Writing around it defeats all four.
- **`protect:` skips, it does not fail.** The glob is the operator naming paths
  another tool owns — apt's signed `dists/`, dnf's `repodata/` — so refusing to
  write there is the request, not a fault. A protected path is never recorded as
  written, so no later run can overwrite it and `Prune` cannot delete it.
- **Every exit from `build.Run` records ownership,** including the error path
  (`SavePartial`). A build that dies mid-write otherwise leaves files nothing
  claims, and `on_conflict: error` then refuses every later run. The partial save
  unions in the previous run's claims: saving only what a failed run wrote
  disowns the files it never reached.
- **What a listing leaves out is stated, never assumed.** `hide:` is a list of
  globs; the default is `["**/.*"]`, the filesystem's own convention and nothing
  else. Baking in the underscore convention beside the dot leaves no setting
  that describes a tree holding both a published `_tradewars/` and a `.DS_Store`.
  cairn cannot know which prefixes mean "internal" in someone else's tree.
- **A sidecar's `hidden:`, `weight:` and `url:` are honoured, not just parsed.**
  Weight is applied after the walk, so `build` re-sorts: the walker orders
  entries before metadata is merged, and a unit test on `sortEntries` passes
  while the built output is unordered.
- **The HTML is capped, the machine formats never are.** `max_rendered` (1,000
  by default) bounds the rows on a page, so a directory's `index.html` is a
  fixed size whether it holds a thousand entries or fifty thousand; uncapped, a
  50,000-entry directory renders 34 MB. `index.json`, `index.csv` and `index.txt` always
  carry every entry. A capped page states the count and points at `index.txt`,
  and the styled filter's placeholder narrows to match what it can actually
  search.
- **`os.Lstat`, not `os.Stat`,** when testing an output path — a symlink there
  is a conflict, not something to write through.
- **CSV fields starting `= + - @` get an apostrophe.** Spreadsheets execute
  them otherwise, and filenames in a mirror are attacker-influenced.
- **`template.URL` and `safeHTML` need a justification and a hostile-input
  test.** They suppress escaping.
- **`SHA256SUMS` is coreutils format** and is tested against the real
  `sha256sum -c`, not asserted.
- **No hardcoded URLs or ports.** They go in a defaults file or at the top of
  the file that owns them. If a port is taken, stop and ask.

## Logging

Telemetry comes from `provide-telemetry`, and `internal/obs` is the **only**
file that imports it. Everything else takes a `*slog.Logger` — standard library
— so a consumer vendoring the Hugo module, or a test, never pulls a telemetry
stack in behind it, and swapping backends touches one file.

Never `fmt.Println` for diagnostics. `obs.Discard()` is the logger for tests and
for callers that have not set telemetry up, so no call site needs a nil check.

## A recurring false positive

`detect-secrets` flags SHA-256 digests as high-entropy strings. In a project
whose whole point is publishing checksums, that will happen often. Mark them
with an inline `# pragma: allowlist secret` and a note saying why — a published
checksum exists to be compared against in public. Never widen the baseline to
silence the class.

## Testing

TDD: write the failing test, watch it fail for the right reason, then
implement. A test that has never failed has never been verified.

- Golden-file tests per emitter — fixture tree in, exact bytes out.
- Table-driven cases for anything with boundaries (sizes, precedence, globs).
- Hostile input is a test case, not a review note.
- `make cover` floors coverage at 90% over `./internal/...`. A
  declaration-only package reports "no statements" and passes — that is
  intentional and distinguished from 0% of real statements.

## CI

Four jobs in `.github/workflows/ci.yml`: `test` (3 OSes), `lint`, `security`,
`hygiene`. Every step has a comment saying what it does. **No `run:` block
exceeds three lines** — anything longer is a script in `ci/`.

Run it locally before pushing:

```sh
make act          # lint, security, hygiene + the ubuntu test leg
make act-job JOB=security
```

`act` cannot run the Windows or macOS legs; `ci/act.sh` filters the matrix to
ubuntu. Needs Docker running (colima is fine).

## Commits

Conventional Commits, enforced by `commitlint` on `commit-msg`.
`feat:`, `fix:`, `docs:`, `build:`, `test:`, `refactor:`, `chore:`.

Subject ≤ 50 chars where it reads naturally. Body explains **why**, not what —
the diff already says what. Commits are signed (SSH). Never bypass signing with
`--no-gpg-sign` or `--no-verify`; if signing stalls, stop and ask.

No AI or assistant attribution in commit messages or PR bodies.

## SPDX

Every Go, shell and Make file opens with:

```go
// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT
```

Files that cannot carry a comment are covered by `REUSE.toml`. `reuse` runs in
the `hygiene` job and in pre-commit.

## Handoff

Mid-stream state goes in `.provide/HANDOFF.md`: what was asked, what changed,
why that approach, and a checklist for whoever picks it up.
