// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildengine

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHashContextDeterministic(t *testing.T) {
	d1 := t.TempDir()
	writeFile(t, d1, "a.txt", "hello")
	writeFile(t, d1, "sub/b.txt", "world")

	d2 := t.TempDir()
	// same content, written in a different order
	writeFile(t, d2, "sub/b.txt", "world")
	writeFile(t, d2, "a.txt", "hello")

	h1, err := HashContext(d1, "")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashContext(d2, "")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("expected identical hashes, got %s vs %s", h1, h2)
	}
}

func TestHashContextChangesWithContent(t *testing.T) {
	d := t.TempDir()
	writeFile(t, d, "a.txt", "hello")
	before, _ := HashContext(d, "")
	writeFile(t, d, "a.txt", "changed")
	after, _ := HashContext(d, "")
	if before == after {
		t.Fatal("expected hash to change when content changes")
	}
}

func TestHashContextRespectsDockerignore(t *testing.T) {
	d := t.TempDir()
	writeFile(t, d, "keep.txt", "keep")
	writeFile(t, d, "ignore.log", "noise")
	writeFile(t, d, ".dockerignore", "*.log\n")

	withIgnore, err := HashContext(d, "")
	if err != nil {
		t.Fatal(err)
	}

	// Changing an ignored file must not change the hash.
	writeFile(t, d, "ignore.log", "different noise")
	again, err := HashContext(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if withIgnore != again {
		t.Fatal("changing an ignored file must not affect the context hash")
	}

	// Changing a kept file must change the hash.
	writeFile(t, d, "keep.txt", "modified")
	changed, _ := HashContext(d, "")
	if changed == withIgnore {
		t.Fatal("changing a kept file must change the context hash")
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"https://index.docker.io/v1/":          "docker.io",
		"registry-1.docker.io":                 "docker.io",
		"docker.io":                            "docker.io",
		"https://ghcr.io":                      "ghcr.io",
		"123.dkr.ecr.us-east-1.amazonaws.com/": "123.dkr.ecr.us-east-1.amazonaws.com",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}
