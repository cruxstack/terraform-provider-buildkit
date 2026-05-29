// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildkitbin

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// wanted is the set of binaries we extract from the release tarballs. Other
// entries (CNI plugins, qemu emulators, rootlessctl, docker-proxy) are skipped
// to keep the cache small; buildkit-runc is required because buildkitd execs it.
var wanted = map[string]bool{
	"buildkitd":     true,
	"buildkit-runc": true,
	"rootlesskit":   true,
}

// pathTransform normalizes an archive entry name to a destination base name, or
// returns ok=false to skip the entry.
type pathTransform func(name string) (base string, ok bool)

// stripBinPrefix maps "bin/buildkitd" -> "buildkitd" for the buildkit tarballs,
// which nest everything under a leading bin/ directory.
func stripBinPrefix(name string) (string, bool) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "bin/")
	base := filepath.Base(name)
	return base, wanted[base]
}

// noStrip maps archive entries that live at the archive root (rootlesskit),
// keeping only the wanted binaries.
func noStrip(name string) (string, bool) {
	base := filepath.Base(filepath.ToSlash(name))
	return base, wanted[base]
}

// extractTarGz extracts the wanted regular-file entries from a gzipped tar
// stream into dir, flattening paths via transform and marking them executable.
func extractTarGz(r io.Reader, dir string, transform pathTransform) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base, ok := transform(hdr.Name)
		if !ok {
			continue
		}
		if err := writeFile(filepath.Join(dir, base), tr); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes content to a temp file in the same directory and atomically
// renames it into place with executable permissions.
func writeFile(dst string, content io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".extract-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, content); err != nil { //nolint:gosec // sizes bounded by pinned, checksum-verified release artifacts
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// lookPath is a thin wrapper around exec.LookPath, isolated so the resolver can
// be unit-tested and to keep the os/exec import local to this file.
func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}
