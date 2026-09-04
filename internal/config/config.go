// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// SupportedVersion is the only cairn.yaml schema version this build accepts.
const SupportedVersion = 1

// Conflict policies for an output path that already exists.
const (
	ConflictError = "error"
	ConflictSkip  = "skip"
)

// Output modes. In direct mode cairn writes index.{json,csv,html} itself. In
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

// Config is a parsed root cairn.yaml.
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
	Defaults       Override `yaml:"defaults"`
	Rules          []Rule   `yaml:"rules"`
}

// Load reads and validates a root cairn.yaml.
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
	if err := yaml.Unmarshal(b, &c); err != nil {
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
// one, empty when unset. Doing it once here means every path cairn emits can
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
	if err := validateSources(p, c.Defaults, c.Rules); err != nil {
		return err
	}
	if err := validateHide(p, c.Defaults, c.Rules); err != nil {
		return err
	}
	if c.Mode != ModeDirect && c.Mode != ModeHugo {
		return fmt.Errorf("config %s: mode must be %s or %s, got %q",
			p, ModeDirect, ModeHugo, c.Mode)
	}
	return nil
}

// validateSources rejects an unknown source anywhere in the config. Without
// this a typo falls through to the fs default and silently indexes the wrong
// thing, which is worse than refusing to start.
func validateSources(p string, defaults Override, rules []Rule) error {
	check := func(o Override) error {
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

// Resolve returns the effective Settings for relDir. Precedence, lowest first:
// built-in defaults, root defaults:, each matching rule in file order, then the
// directory's own .cairn.yaml.
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

// validateHide rejects a config that cannot mean what it says: a glob that will
// never compile, or the removed hidden: key.
func validateHide(p string, defaults Override, rules []Rule) error {
	check := func(o Override) error {
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
