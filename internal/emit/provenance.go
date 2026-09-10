// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/json"
	"fmt"
)

// Provenance is a build-attestation sidecar: which cairndex version and
// which config produced this tree, when, and a digest of every file this
// run wrote, so a verifier need not trust the tree's own bytes on faith.
// Shaped like SLSA's provenance subject list (name+digest pairs) rather
// than one aggregate hash, so a mismatch names the exact file that
// changed. ConfigSHA256 alone is checkable with nothing but `sha256sum
// cairndex.yaml`; Outputs is JSON, not coreutils' sha256sum format, so
// verifying it takes one small reshape first — see
// docs/deployment/reference.md's "Build provenance" for the actual recipe.
type Provenance struct {
	CairndexVersion string           `json:"cairndex_version"`
	ConfigSHA256    string           `json:"config_sha256,omitempty"`
	StartedAt       string           `json:"started_at"`
	FinishedAt      string           `json:"finished_at"`
	Dirs            int              `json:"dirs"`
	Files           int              `json:"files"`
	Outputs         []ProvenanceFile `json:"outputs"`
}

// ProvenanceFile is one output cairndex wrote this run, path relative to
// out:.
type ProvenanceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest renders p as cairndex-manifest.json.
//
// Outputs is never nil going in — the caller always passes at least an
// empty slice — so this never has to choose between "outputs": null and
// "outputs": [] on p's behalf.
func Manifest(p Provenance) ([]byte, error) {
	if p.Outputs == nil {
		p.Outputs = []ProvenanceFile{}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode provenance manifest: %w", err)
	}
	return append(b, '\n'), nil
}
