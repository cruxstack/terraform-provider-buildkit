// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

// Package buildkitbin provisions the BuildKit binaries (buildkitd, the bundled
// buildkit-runc, and rootlesskit) needed to run an embedded BuildKit daemon on
// Linux without requiring the user to preinstall Docker or BuildKit.
//
// Resolution for each binary is, in order:
//
//  1. an explicit override directory (BUILDKIT_EMBEDDED_BIN_DIR), for air-gapped
//     or BYO-binary setups;
//  2. a provider-managed cache directory populated by a previous run;
//  3. the host PATH (preserves prior behaviour and respects system installs);
//  4. download of a pinned, checksum-verified release tarball into the cache.
//
// The downloaded artifacts are pinned by version and SHA256 (see pins.go) and
// every download is verified before extraction.
package buildkitbin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"
)

// envOverrideDir lets users point the provider at a directory that already
// contains the required binaries (buildkitd, buildkit-runc, rootlesskit),
// bypassing all downloading. Useful for air-gapped CI.
const envOverrideDir = "BUILDKIT_EMBEDDED_BIN_DIR"

// Tools is the set of resolved absolute paths the supervisor needs.
type Tools struct {
	// BinDir is a directory containing buildkitd and buildkit-runc. It should be
	// prepended to the daemon's PATH so buildkitd can exec its runc.
	BinDir string
	// Buildkitd is the absolute path to the buildkitd binary.
	Buildkitd string
	// Rootlesskit is the absolute path to the rootlesskit binary. Empty when not
	// requested (i.e. when running as root).
	Rootlesskit string
	// Source describes how the binaries were obtained, for logging.
	Source string
}

// Options controls provisioning behaviour.
type Options struct {
	// NeedRootlesskit requests resolution of rootlesskit in addition to
	// buildkitd. Set when the daemon will run unprivileged.
	NeedRootlesskit bool
	// AllowDownload permits fetching pinned release tarballs when the binaries
	// are not already present. When false, only the override dir, cache, and PATH
	// are consulted.
	AllowDownload bool
}

// Ensure resolves (and, if permitted, downloads) the binaries described by opts.
func Ensure(ctx context.Context, opts Options) (*Tools, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("embedded buildkit binaries are only available on linux, not %s", runtime.GOOS)
	}
	arch := runtime.GOARCH
	if _, ok := buildkitArtifacts[arch]; !ok {
		return nil, fmt.Errorf("no pinned buildkit release for linux/%s; install buildkitd manually or set %s", arch, envOverrideDir)
	}

	// 1. explicit override directory.
	if dir := os.Getenv(envOverrideDir); dir != "" {
		t, err := fromDir(dir, opts.NeedRootlesskit)
		if err != nil {
			return nil, fmt.Errorf("%s=%q is set but is missing required binaries: %w", envOverrideDir, dir, err)
		}
		t.Source = envOverrideDir + "=" + dir
		return t, nil
	}

	cacheDir, err := cacheDirFor(arch)
	if err != nil {
		return nil, err
	}

	// 2. previously populated cache.
	if t, err := fromDir(cacheDir, opts.NeedRootlesskit); err == nil {
		t.Source = "cache (" + cacheDir + ")"
		return t, nil
	}

	// 3. host PATH.
	if t, ok := fromPath(opts.NeedRootlesskit); ok {
		t.Source = "PATH"
		return t, nil
	}

	// 4. download.
	if !opts.AllowDownload {
		return nil, fmt.Errorf(
			"buildkit binaries not found in %s, the cache, or PATH, and downloading is disabled; "+
				"install buildkitd (and rootlesskit for unprivileged use) or set %s",
			envOverrideDir, envOverrideDir,
		)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating buildkit cache dir: %w", err)
	}

	// serialize downloads across concurrent provider invocations / resources.
	lock := flock.New(filepath.Join(cacheDir, ".lock"))
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if locked, err := lock.TryLockContext(lockCtx, 200*time.Millisecond); err != nil || !locked {
		return nil, fmt.Errorf("acquiring buildkit cache lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	// re-check the cache after acquiring the lock: another process may have
	// populated it while we waited.
	if t, err := fromDir(cacheDir, opts.NeedRootlesskit); err == nil {
		t.Source = "cache (" + cacheDir + ")"
		return t, nil
	}

	if err := downloadAndExtract(ctx, buildkitArtifacts[arch], cacheDir, stripBinPrefix); err != nil {
		return nil, fmt.Errorf("provisioning buildkit %s: %w", buildkitVersion, err)
	}
	if opts.NeedRootlesskit {
		rk, ok := rootlesskitArtifacts[arch]
		if !ok {
			return nil, fmt.Errorf("no pinned rootlesskit release for linux/%s; run as root or set %s", arch, envOverrideDir)
		}
		if err := downloadAndExtract(ctx, rk, cacheDir, noStrip); err != nil {
			return nil, fmt.Errorf("provisioning rootlesskit %s: %w", rootlesskitVersion, err)
		}
	}

	t, err := fromDir(cacheDir, opts.NeedRootlesskit)
	if err != nil {
		return nil, fmt.Errorf("binaries missing after provisioning into %s: %w", cacheDir, err)
	}
	t.Source = fmt.Sprintf("downloaded buildkit %s (%s)", buildkitVersion, cacheDir)
	return t, nil
}

// fromDir builds a Tools from a directory if it contains the required binaries.
func fromDir(dir string, needRootlesskit bool) (*Tools, error) {
	bkd := filepath.Join(dir, "buildkitd")
	if !isExecutable(bkd) {
		return nil, fmt.Errorf("buildkitd not found in %s", dir)
	}
	t := &Tools{BinDir: dir, Buildkitd: bkd}
	if needRootlesskit {
		rk := filepath.Join(dir, "rootlesskit")
		if !isExecutable(rk) {
			return nil, fmt.Errorf("rootlesskit not found in %s", dir)
		}
		t.Rootlesskit = rk
	}
	return t, nil
}

// fromPath resolves binaries from the host PATH.
func fromPath(needRootlesskit bool) (*Tools, bool) {
	bkd, err := lookPath("buildkitd")
	if err != nil {
		return nil, false
	}
	t := &Tools{BinDir: filepath.Dir(bkd), Buildkitd: bkd}
	if needRootlesskit {
		rk, err := lookPath("rootlesskit")
		if err != nil {
			return nil, false
		}
		t.Rootlesskit = rk
	}
	return t, true
}

// cacheDirFor returns the versioned, arch-specific cache directory.
func cacheDirFor(arch string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = filepath.Join(os.TempDir(), "terraform-provider-buildkit-cache")
	}
	// include both tool versions so a bump invalidates the cache cleanly.
	ver := buildkitVersion + "_rk-" + rootlesskitVersion
	return filepath.Join(base, "terraform-provider-buildkit", "bin", ver, "linux-"+arch), nil
}

// downloadAndExtract fetches art, verifies its SHA256, and extracts it into dir.
func downloadAndExtract(ctx context.Context, art artifact, dir string, transform pathTransform) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, art.url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", art.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: unexpected status %s", art.url, resp.Status)
	}

	// stream to a temp file while hashing, then verify before extracting.
	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("reading %s: %w", art.url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != art.sha256 {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", art.url, art.sha256, got)
	}

	f, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return extractTarGz(f, dir, transform)
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}
