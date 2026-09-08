// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// SupportedVersion is the only cairndex.yaml schema version this build accepts.
const SupportedVersion = 1

// Conflict policies for an output path that already exists.
const (
	ConflictError = "error"
	ConflictSkip  = "skip"
)

// Output modes. In direct mode cairndex writes index.{json,csv,html} itself. In
// hugo mode it writes one _index.md per directory and Hugo renders the formats
// from that single source, so they cannot drift.
const (
	ModeDirect = "direct"
	ModeHugo   = "hugo"
)

// Built-in policy values applied when a root config omits them.
const (
	DefaultIndexBasename  = "index"
	DefaultTreeMaxEntries = 50000
)

// Rule is a path-glob-scoped partial override of Settings.
type Rule struct {
	Match    string `yaml:"match"`
	Override `yaml:",inline"`
}

// Config is a parsed root cairndex.yaml.
type Config struct {
	Version        int      `yaml:"version"`
	Mode           string   `yaml:"mode"`
	Root           string   `yaml:"root"`
	BasePath       string   `yaml:"base_path"`
	Out            string   `yaml:"out"`
	IndexBasename  string   `yaml:"index_basename"`
	TreeMaxEntries int      `yaml:"tree_max_entries"`
	Protect        []string `yaml:"protect"`
	OnConflict     string   `yaml:"on_conflict"`
	// BuildInfo shows a small footer banner — the build's own timestamp and
	// cairndex version — on every listing page. On by default: nil and
	// true both mean show it, so ShowBuildInfo is what every reader checks
	// rather than this field directly, and build_info: false is the only
	// way to turn it off. Root-level rather than in Override: what built a
	// run is one fact about the whole run, not a per-directory setting.
	BuildInfo *bool    `yaml:"build_info"`
	Defaults  Override `yaml:"defaults"`
	Rules     []Rule   `yaml:"rules"`
}

// ShowBuildInfo reports whether a listing page should carry the build-info
// footer. A pointer only so build_info: false is distinguishable from
// build_info: absent — checking it directly at every call site would be a
// nil check repeated everywhere this matters instead of once here.
func (c *Config) ShowBuildInfo() bool {
	return deref(c.BuildInfo, true)
}

// Load reads and validates a root cairndex.yaml.
func Load(p string) (*Config, error) {
	// #nosec G304 -- p is the config path the operator passed on the command
	// line. Reading the file the user named is the function's entire purpose;
	// there is no trust boundary here to cross. Reads of paths derived from
	// scanned content are a different matter and are contained by emit.Write.
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	// Strict. A key this build does not recognise is a typo, and the decoder's
	// default is to drop it without a word — so the build runs with defaults the
	// operator never asked for and reports success. checksum: sha-256 wrote no
	// SHA256SUMS at all and exited zero; a rule whose match: was mistyped
	// matched nothing. Refusing costs one clear error naming the line.
	dec.KnownFields(true)
	// io.EOF is an empty document, not a failure to parse. It falls through to
	// validate, which refuses it for the version it does not declare.
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	c.applyPolicyDefaults()
	c.normalizeBasePath()
	if err := c.validate(p); err != nil {
		return nil, err
	}
	return &c, nil
}

// applyPolicyDefaults fills the top-level policy fields a config may omit.
// normalizeBasePath makes base_path a clean prefix: leading slash, no trailing
// one, empty when unset. Doing it once here means every path cairndex emits can
// concatenate without re-deciding what shape the value was written in.
func (c *Config) normalizeBasePath() {
	p := strings.Trim(c.BasePath, "/")
	if p == "" {
		c.BasePath = ""
		return
	}
	c.BasePath = "/" + p
}

func (c *Config) applyPolicyDefaults() {
	if c.IndexBasename == "" {
		c.IndexBasename = DefaultIndexBasename
	}
	if c.TreeMaxEntries == 0 {
		c.TreeMaxEntries = DefaultTreeMaxEntries
	}
	if c.OnConflict == "" {
		c.OnConflict = ConflictError
	}
	if c.Mode == "" {
		c.Mode = ModeDirect
	}
}

// validate rejects a config this build cannot honor. It is separate from
// applyPolicyDefaults so neither function carries both jobs, and so the
// version check reads before any value is trusted.
func (c *Config) validate(p string) error {
	if c.Version != SupportedVersion {
		return fmt.Errorf("config %s: version %d unsupported, this build accepts %d",
			p, c.Version, SupportedVersion)
	}
	if c.OnConflict != ConflictError && c.OnConflict != ConflictSkip {
		return fmt.Errorf("config %s: on_conflict must be %s or %s, got %q",
			p, ConflictError, ConflictSkip, c.OnConflict)
	}
	if err := validateOverrides(p, c.Defaults, c.Rules); err != nil {
		return err
	}
	if c.Mode != ModeDirect && c.Mode != ModeHugo {
		return fmt.Errorf("config %s: mode must be %s or %s, got %q",
			p, ModeDirect, ModeHugo, c.Mode)
	}
	return nil
}

// ValidateOverride runs the same checks the root cairndex.yaml's defaults: and
// rules go through against one override on its own, for a caller reading a
// directory's own .cairndex.yaml. Without this, a checksum: typo there does
// not fail the build the way the identical typo at the root does — it just
// never gives that directory's entries a digest, and SHA256SUMS is not
// written for it, silently.
func ValidateOverride(p string, o Override) error {
	return validateOverrides(p, o, nil)
}

// validateOverrides runs every override-level check against defaults: and
// rules — or, from ValidateOverride, a single directory override on its
// own. One eachOverride walk running all five checks per override, rather
// than each check walking defaults: and rules on its own: five walks over
// the same rule list on every config load and one root cairndex.yaml, doing
// nothing an extra field on this function couldn't, was the cost of adding
// each new validator here as its own top-level pass.
func validateOverrides(p string, defaults Override, rules []Rule) error {
	return eachOverride(defaults, rules, func(o Override) error {
		if err := validateSource(p, o); err != nil {
			return err
		}
		if err := validateHideOverride(p, o); err != nil {
			return err
		}
		if err := validateChecksum(p, o); err != nil {
			return err
		}
		if err := validatePEP503Level(p, o); err != nil {
			return err
		}
		return validateOutputConflict(p, o)
	})
}

// eachOverride runs check against the root defaults and every rule. The three
// validators below all ask the same question in two places, and writing the
// walk once is what stops one of them growing a third place to look and the
// others not.
func eachOverride(defaults Override, rules []Rule, check func(Override) error) error {
	if err := check(defaults); err != nil {
		return err
	}
	for _, r := range rules {
		if err := check(r.Override); err != nil {
			return err
		}
	}
	return nil
}

// validateChecksum rejects an unknown digest on one override.
//
// This was the one setting nothing checked, and it fails in the worst possible
// direction: with outputs: [sums] and a mistyped algorithm, no entry is ever
// given a digest, so SHA256SUMS is not written at all and the build reports
// success. A mirror serving no integrity when it was configured for integrity
// is the failure this project exists to prevent.
func validateChecksum(p string, o Override) error {
	if o.Checksum == nil {
		return nil
	}
	switch *o.Checksum {
	case ChecksumNone, ChecksumSHA256:
		return nil
	}
	return fmt.Errorf("config %s: checksum must be %s or %s, got %q",
		p, ChecksumNone, ChecksumSHA256, *o.Checksum)
}

// validatePEP503Level rejects an unknown pep503_level on one override. Left
// unset it is not a mistake — emit.PEP503Mixed's warning covers that case —
// so only a value that names neither level is refused.
func validatePEP503Level(p string, o Override) error {
	if o.PEP503Level == nil {
		return nil
	}
	switch *o.PEP503Level {
	case PEP503LevelRoot, PEP503LevelProject:
		return nil
	}
	return fmt.Errorf("config %s: pep503_level must be %s or %s, got %q",
		p, PEP503LevelRoot, PEP503LevelProject, *o.PEP503Level)
}

// outputTargets maps an output format that renders the index page itself to
// the file it writes there. Two formats sharing a target conflict: both try
// to own the same URL within one directory, and only one write can survive
// it — html and pep503 both land on index.html today, and a format added
// later that also renders the index page joins this table rather than
// needing its own bespoke pairwise check.
var outputTargets = map[string]string{
	OutputHTML:   "index.html",
	OutputPEP503: "index.html",
}

// validateOutputConflict rejects an outputs: list asking for two formats
// that render the same target. mode: direct's write guard happens to catch
// two writes landing on one path within a run, but mode: hugo skips both
// from its own per-format write path entirely, relying on its template to
// decide which one wins, which it does silently. Refused up front instead,
// so the rule holds the same way in both modes rather than one enforcing it
// as a side effect and the other not at all.
func validateOutputConflict(p string, o Override) error {
	if o.Outputs == nil {
		return nil
	}
	byTarget := map[string][]string{}
	for _, out := range *o.Outputs {
		if target, ok := outputTargets[out]; ok {
			byTarget[target] = append(byTarget[target], out)
		}
	}
	for target, names := range byTarget {
		if len(names) > 1 {
			return fmt.Errorf("config %s: outputs: cannot ask for both %s, they all render %s",
				p, strings.Join(names, " and "), target)
		}
	}
	return nil
}

// validateSource rejects an unknown source on one override. Without this a
// typo falls through to the fs default and silently indexes the wrong
// thing, which is worse than refusing to start.
func validateSource(p string, o Override) error {
	if o.Source == nil {
		return nil
	}
	switch *o.Source {
	case SourceFS, SourcePages, SourceManifest:
		return nil
	}
	return fmt.Errorf("config %s: source must be %s, %s or %s, got %q",
		p, SourceFS, SourcePages, SourceManifest, *o.Source)
}

// Resolve returns the effective Settings for relDir. Precedence, lowest first:
// built-in defaults, root defaults:, each matching rule in file order, then the
// directory's own .cairndex.yaml.
func (c *Config) Resolve(relDir string, dirOverride *Override) Settings {
	s := c.Defaults.Apply(Defaults())
	for _, r := range c.Rules {
		if matchDir(r.Match, relDir) {
			s = r.Apply(s)
		}
	}
	if dirOverride != nil {
		s = dirOverride.Apply(s)
	}
	return s
}

// IsProtected reports whether relPath matches any protect: glob. A malformed
// glob matches nothing rather than everything — a pattern typo must not silently
// block every write.
func (c *Config) IsProtected(relPath string) bool {
	for _, g := range c.Protect {
		if ok, err := doublestar.Match(g, relPath); err == nil && ok {
			return true
		}
	}
	return false
}

// validateHideOverride rejects a config that cannot mean what it says: a
// glob that will never compile, or the removed hidden: key.
func validateHideOverride(p string, o Override) error {
	if o.Hidden != nil {
		return fmt.Errorf("config %s: hidden: has been replaced by hide:, "+
			"a list of globs matched against the path relative to root "+
			"(the old default is hide: [%q])", p, DefaultHideGlob)
	}
	if o.Hide == nil {
		return nil
	}
	for _, g := range *o.Hide {
		if _, err := doublestar.Match(g, "probe"); err != nil {
			return fmt.Errorf("config %s: hide: %q is not a valid glob: %w", p, g, err)
		}
	}
	return nil
}

// matchDir reports whether a rule glob covers a directory. A glob such as
// "bootstrap/**" is written to describe the contents of bootstrap, so the
// directory itself matches too — otherwise the directory holding the files
// would be configured differently from the files in it.
func matchDir(glob, relDir string) bool {
	if ok, err := doublestar.Match(glob, relDir); err == nil && ok {
		return true
	}
	if ok, err := doublestar.Match(glob, path.Join(relDir, "x")); err == nil && ok {
		return true
	}
	return false
}
