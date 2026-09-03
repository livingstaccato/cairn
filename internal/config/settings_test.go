// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

// These tests assert literal strings rather than the exported constants on
// purpose. Comparing a constant against itself passes even when its value is
// changed wrongly, and these values are a config-file contract: "styled" in
// someone's cairn.yaml has to keep meaning what it means.

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func TestOverrideApply(t *testing.T) {
	base := Settings{
		Source: "fs", Present: "styled", Outputs: []string{"html", "json", "csv"},
		Sort: "name", Order: "asc", DirsFirst: true, Hidden: "skip",
		Checksum: "none", Recursive: false, FollowSymlinks: false,
	}

	t.Run("empty override changes nothing", func(t *testing.T) {
		got := Override{}.Apply(base)
		if got.Present != "styled" || !got.DirsFirst || got.Checksum != "none" {
			t.Errorf("empty override mutated base: %+v", got)
		}
	})

	t.Run("set fields win", func(t *testing.T) {
		got := Override{
			Present:  strp("bare"),
			Checksum: strp("sha256"),
			Outputs:  &[]string{"html", "json", "csv", "sums"},
		}.Apply(base)
		if got.Present != "bare" {
			t.Errorf("Present = %q, want bare", got.Present)
		}
		if got.Checksum != "sha256" {
			t.Errorf("Checksum = %q, want sha256", got.Checksum)
		}
		if len(got.Outputs) != 4 {
			t.Errorf("Outputs = %v, want 4 entries", got.Outputs)
		}
		if got.Source != "fs" {
			t.Errorf("Source = %q, want fs preserved from base", got.Source)
		}
	})

	// The reason Override uses pointers at all.
	t.Run("explicit false is distinguishable from unset", func(t *testing.T) {
		got := Override{DirsFirst: boolp(false)}.Apply(base)
		if got.DirsFirst {
			t.Error("explicit false was ignored; unset and false must differ")
		}
	})

	t.Run("base is not mutated", func(t *testing.T) {
		_ = Override{Present: strp("bare")}.Apply(base)
		if base.Present != "styled" {
			t.Errorf("Apply mutated its receiver's argument: %q", base.Present)
		}
	})

	// Outputs must be copied, not aliased: two directories resolving from the
	// same override would otherwise share one backing array.
	t.Run("outputs slice is copied", func(t *testing.T) {
		outs := []string{"html", "json"}
		got := Override{Outputs: &outs}.Apply(base)
		outs[0] = "mutated"
		if got.Outputs[0] != "html" {
			t.Errorf("Outputs aliased the override's slice: %v", got.Outputs)
		}
	})
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Source != "fs" || d.Present != "styled" || d.Sort != "name" ||
		d.Order != "asc" || !d.DirsFirst || d.Hidden != "skip" || d.Checksum != "none" {
		t.Errorf("unexpected defaults: %+v", d)
	}
	if len(d.Outputs) != 3 {
		t.Errorf("default Outputs = %v, want [html json csv]", d.Outputs)
	}
	if d.Recursive || d.FollowSymlinks {
		t.Error("Recursive and FollowSymlinks must default false")
	}
}

// Defaults returns a fresh value each call; one caller must not be able to
// change what the next caller sees.
func TestDefaultsNotShared(t *testing.T) {
	a := Defaults()
	a.Outputs[0] = "mutated"
	if Defaults().Outputs[0] != "html" {
		t.Error("Defaults shares its Outputs backing array between calls")
	}
}
