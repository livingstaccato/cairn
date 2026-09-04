// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

func TestSumsFormat(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "zeta.sh", SHA256: strings.Repeat("b", 64)},
		{Name: "alpha.iso", SHA256: strings.Repeat("a", 64)},
		{Name: "nested", IsDir: true, SHA256: ""},
		{Name: "unhashed.txt", SHA256: ""},
	}}
	got := string(Sums(l))
	want := strings.Repeat("a", 64) + "  alpha.iso\n" +
		strings.Repeat("b", 64) + "  zeta.sh\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, "nested") {
		t.Error("directories must not appear in SHA256SUMS")
	}
	if strings.Contains(got, "unhashed") {
		t.Error("entries without a digest must be omitted")
	}
}

func TestSumsEmptyWhenNothingHashed(t *testing.T) {
	if got := Sums(model.Listing{Entries: []model.Entry{{Name: "a"}}}); len(got) != 0 {
		t.Errorf("got %q, want empty", got)
	}
}

// The compatibility claim, tested against the real tool rather than asserted.
func TestSumsVerifiesWithCoreutils(t *testing.T) {
	bin, err := exec.LookPath("sha256sum")
	if err != nil {
		if bin, err = exec.LookPath("gsha256sum"); err != nil {
			t.Skip("no sha256sum or gsha256sum on PATH")
		}
	}

	dir := t.TempDir()
	var entries []model.Entry
	for name, body := range map[string]string{
		"bootstrap.sh": "#!/bin/sh\necho hi\n",
		"apt.list":     "deb http://example.invalid stable main\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		entries = append(entries, model.Entry{Name: name, SHA256: hex.EncodeToString(sum[:])})
	}

	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"),
		Sums(model.Listing{Entries: entries}), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-c", "SHA256SUMS")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s -c failed: %v\n%s", bin, err, out)
	}
}
