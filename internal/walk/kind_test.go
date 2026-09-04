// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package walk

import "testing"

func TestKindOf(t *testing.T) {
	cases := []struct {
		name  string
		isDir bool
		kind  string
	}{
		{"linux", true, "dir"},
		{"bootstrap.sh", false, "script"},
		{"setup.ps1", false, "script"},
		{"ubuntu-24.04.iso", false, "image"},
		{"disk.qcow2", false, "image"},
		{"archive.tar.gz", false, "archive"},
		{"pkg.deb", false, "archive"},
		{"pkg.rpm", false, "archive"},
		{"README.md", false, "doc"},
		{"index.html", false, "page"},
		{"cloud-init.yaml", false, "config"},
		{"hosts.toml", false, "config"},
		{"inventory.json", false, "data"},
		{"report.csv", false, "data"},
		{"mystery.zzz", false, "other"},
		{"NOEXTENSION", false, "other"},
		{"UPPER.ISO", false, "image"},
	}
	for _, c := range cases {
		got, _ := KindOf(c.name, c.isDir)
		if got != c.kind {
			t.Errorf("KindOf(%q, %v) kind = %q, want %q", c.name, c.isDir, got, c.kind)
		}
	}
}

func TestKindOfMIME(t *testing.T) {
	if _, mime := KindOf("page.html", false); mime == "" {
		t.Error("expected a MIME type for .html")
	}
	if _, mime := KindOf("linux", true); mime != "inode/directory" {
		t.Errorf("directory MIME = %q, want inode/directory", mime)
	}
	if _, mime := KindOf("mystery.zzz", false); mime != "application/octet-stream" {
		t.Errorf("unknown MIME = %q, want application/octet-stream", mime)
	}
}

// "archive.tar.gz" must read as .tar.gz, not as .gz — the compound extension
// is the one that says what the file actually is.
func TestKindOfCompoundExtension(t *testing.T) {
	for _, n := range []string{"a.tar.gz", "a.tar.xz", "a.tar.zst", "a.tar.bz2"} {
		if k, _ := KindOf(n, false); k != "archive" {
			t.Errorf("KindOf(%q) = %q, want archive", n, k)
		}
	}
}
