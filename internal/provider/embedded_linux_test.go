// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubIDFileMentions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subuid")
	content := "" +
		"# a comment\n" +
		"\n" +
		"root:0:1\n" +
		"alice:100000:65536\n" +
		"1001:200000:65536\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, uid string
		want      bool
	}{
		{"alice", "0", true},   // matched by name
		{"bob", "1001", true},  // matched by uid
		{"bob", "9999", false}, // neither
		{"root", "0", true},    // by name
	}
	for _, c := range cases {
		if got := subIDFileMentions(path, c.name, c.uid); got != c.want {
			t.Errorf("subIDFileMentions(%q,%q,%q) = %v, want %v", path, c.name, c.uid, got, c.want)
		}
	}
}

func TestSubIDFileMentions_MissingFileIsPermissive(t *testing.T) {
	// absent files should not block (managed/namespaced environments).
	if !subIDFileMentions(filepath.Join(t.TempDir(), "nope"), "alice", "1000") {
		t.Errorf("missing subid file should be treated as permissive (true)")
	}
}
