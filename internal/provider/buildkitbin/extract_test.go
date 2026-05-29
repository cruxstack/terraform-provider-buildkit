// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildkitbin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz builds an in-memory .tar.gz from name->content entries.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractTarGz_BuildkitLayout(t *testing.T) {
	data := makeTarGz(t, map[string]string{
		"bin/buildkitd":           "BKD",
		"bin/buildkit-runc":       "RUNC",
		"bin/buildctl":            "CTL",  // not wanted
		"bin/buildkit-qemu-arm":   "QEMU", // not wanted
		"bin/buildkit-cni-bridge": "CNI",  // not wanted
	})
	dir := t.TempDir()
	if err := extractTarGz(bytes.NewReader(data), dir, stripBinPrefix); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	assertFile(t, filepath.Join(dir, "buildkitd"), "BKD", true)
	assertFile(t, filepath.Join(dir, "buildkit-runc"), "RUNC", true)
	assertAbsent(t, filepath.Join(dir, "buildctl"))
	assertAbsent(t, filepath.Join(dir, "buildkit-qemu-arm"))
}

func TestExtractTarGz_RootlesskitLayout(t *testing.T) {
	data := makeTarGz(t, map[string]string{
		"rootlesskit":              "RK",
		"rootlessctl":              "CTL",   // not wanted
		"rootlesskit-docker-proxy": "PROXY", // not wanted
	})
	dir := t.TempDir()
	if err := extractTarGz(bytes.NewReader(data), dir, noStrip); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	assertFile(t, filepath.Join(dir, "rootlesskit"), "RK", true)
	assertAbsent(t, filepath.Join(dir, "rootlessctl"))
	assertAbsent(t, filepath.Join(dir, "rootlesskit-docker-proxy"))
}

func TestPathTransforms(t *testing.T) {
	t.Run("stripBinPrefix", func(t *testing.T) {
		cases := map[string]struct {
			base string
			ok   bool
		}{
			"bin/buildkitd":     {"buildkitd", true},
			"./bin/buildkitd":   {"buildkitd", true},
			"bin/buildkit-runc": {"buildkit-runc", true},
			"bin/buildctl":      {"buildctl", false},
		}
		for in, want := range cases {
			base, ok := stripBinPrefix(in)
			if ok != want.ok || (ok && base != want.base) {
				t.Errorf("stripBinPrefix(%q) = (%q,%v), want (%q,%v)", in, base, ok, want.base, want.ok)
			}
		}
	})
	t.Run("noStrip", func(t *testing.T) {
		if base, ok := noStrip("rootlesskit"); !ok || base != "rootlesskit" {
			t.Errorf("noStrip(rootlesskit) = (%q,%v)", base, ok)
		}
		if _, ok := noStrip("rootlessctl"); ok {
			t.Errorf("noStrip(rootlessctl) should be skipped")
		}
	})
}

func TestFromDir(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "buildkitd"))

	// rootlesskit not required: succeeds.
	if _, err := fromDir(dir, false); err != nil {
		t.Fatalf("fromDir(needRootlesskit=false): %v", err)
	}
	// rootlesskit required but missing: fails.
	if _, err := fromDir(dir, true); err == nil {
		t.Fatalf("fromDir(needRootlesskit=true) expected error when rootlesskit absent")
	}

	writeExec(t, filepath.Join(dir, "rootlesskit"))
	tools, err := fromDir(dir, true)
	if err != nil {
		t.Fatalf("fromDir(needRootlesskit=true): %v", err)
	}
	if tools.Buildkitd == "" || tools.Rootlesskit == "" || tools.BinDir != dir {
		t.Fatalf("unexpected tools: %+v", tools)
	}
}

func assertFile(t *testing.T, path, want string, wantExec bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(b) != want {
		t.Errorf("%s content = %q, want %q", path, b, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if isExec := fi.Mode()&0o111 != 0; isExec != wantExec {
		t.Errorf("%s exec = %v, want %v", path, isExec, wantExec)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent, stat err = %v", path, err)
	}
}

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}
