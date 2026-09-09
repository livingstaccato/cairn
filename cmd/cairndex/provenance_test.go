// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// End-to-end: `cairndex build` with provenance: true actually writes a
// manifest whose config_sha256 matches the real file on disk, independent
// of anything internal/build's own unit tests already exercise in memory.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/build"
	"github.com/livingstaccato/cairndex/internal/emit"
)

func TestBuildWithProvenanceMatchesTheRealConfigFile(t *testing.T) {
	configPath, out := fixture(t)
	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(b, []byte("provenance: true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	if err := runBuild(context.Background(), configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	manifest, err := os.ReadFile(filepath.Join(out, "provenance.json"))
	if err != nil {
		t.Fatalf("provenance.json was not written: %v, stderr:\n%s", err, stderr.String())
	}
	var m emit.Provenance
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("provenance.json does not parse: %v\n%s", err, manifest)
	}

	written, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(written)
	want := hex.EncodeToString(sum[:])
	if m.ConfigSHA256 != want {
		t.Errorf("config_sha256 = %q, want %q (sha256sum %s)", m.ConfigSHA256, want, configPath)
	}
}
