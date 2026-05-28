// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moby/patternmatcher"
)

// HashContext computes a deterministic SHA256 over the contents of a build
// context directory, honoring .dockerignore exclusions. The hash is stable
// across machines: it incorporates each included file's relative path, mode
// bits, and content, walked in sorted order.
//
// dockerignorePath, when non-empty, overrides the default <context>/.dockerignore.
func HashContext(contextDir, dockerignorePath string) (string, error) {
	root, err := filepath.Abs(contextDir)
	if err != nil {
		return "", fmt.Errorf("resolving context: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}

	patterns, err := readDockerignore(root, dockerignorePath)
	if err != nil {
		return "", err
	}
	pm, err := patternmatcher.New(patterns)
	if err != nil {
		return "", fmt.Errorf("parsing .dockerignore patterns: %w", err)
	}

	type entry struct {
		rel  string
		mode os.FileMode
		path string
	}
	var entries []entry

	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// .dockerignore matching uses slash-separated paths.
		matched, err := pm.MatchesOrParentMatches(rel)
		if err != nil {
			return err
		}
		if matched {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		entries = append(entries, entry{rel: rel, mode: fi.Mode(), path: path})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%o\x00", e.rel, e.mode.Perm())
		f, err := os.Open(e.path)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func readDockerignore(root, override string) ([]string, error) {
	path := override
	if path == "" {
		path = filepath.Join(root, ".dockerignore")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if override == "" && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading dockerignore %q: %w", path, err)
	}
	return parseDockerignoreLines(string(data)), nil
}

func parseDockerignoreLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}
