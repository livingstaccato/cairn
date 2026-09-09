// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/json"
	"testing"
)

func TestManifestIsWellFormedJSON(t *testing.T) {
	b, err := Manifest(Provenance{
		CairndexVersion: "v1.2.3",
		ConfigSHA256:    "deadbeef",
		StartedAt:       "2026-09-09T00:00:00Z",
		FinishedAt:      "2026-09-09T00:01:00Z",
		Dirs:            3,
		Files:           7,
		Outputs: []ProvenanceFile{
			{Path: "bootstrap/index.html", SHA256: "aaa"},
			{Path: "docs/index.html", SHA256: "bbb"},
		},
	})
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Manifest produced unparsable JSON: %v\n%s", err, b)
	}
	if got["cairndex_version"] != "v1.2.3" {
		t.Errorf("cairndex_version = %v, want v1.2.3", got["cairndex_version"])
	}
	outputs, ok := got["outputs"].([]any)
	if !ok || len(outputs) != 2 {
		t.Errorf("outputs = %v, want 2 entries", got["outputs"])
	}
}

// A manifest with no configured hash omits the field rather than emitting a
// misleading empty string a verifier might compare against.
func TestManifestOmitsAnUnsetConfigHash(t *testing.T) {
	b, err := Manifest(Provenance{CairndexVersion: "dev", StartedAt: "x", FinishedAt: "y"})
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["config_sha256"]; present {
		t.Errorf("config_sha256 present with no hash configured: %s", b)
	}
}

// An empty build (nothing written) is still a valid manifest -- outputs: []
// rather than the field vanishing, which would make a verifier's own parser
// special-case "the key is missing" versus "the key is an empty list".
func TestManifestWithNoOutputsIsStillValid(t *testing.T) {
	b, err := Manifest(Provenance{CairndexVersion: "dev", StartedAt: "x", FinishedAt: "y"})
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	outputs, ok := got["outputs"].([]any)
	if !ok {
		t.Fatalf("outputs is not a list: %s", b)
	}
	if len(outputs) != 0 {
		t.Errorf("outputs = %v, want empty", outputs)
	}
}
