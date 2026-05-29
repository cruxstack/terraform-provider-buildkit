// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildkitbin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDownloadAndExtract_ChecksumVerified(t *testing.T) {
	payload := makeTarGz(t, map[string]string{"bin/buildkitd": "BKD"})
	sum := sha256.Sum256(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := artifact{url: srv.URL, sha256: hex.EncodeToString(sum[:])}
	if err := downloadAndExtract(context.Background(), art, dir, stripBinPrefix); err != nil {
		t.Fatalf("downloadAndExtract: %v", err)
	}
	assertFile(t, filepath.Join(dir, "buildkitd"), "BKD", true)
}

func TestDownloadAndExtract_ChecksumMismatch(t *testing.T) {
	payload := makeTarGz(t, map[string]string{"bin/buildkitd": "BKD"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := artifact{url: srv.URL, sha256: "deadbeef"}
	err := downloadAndExtract(context.Background(), art, dir, stripBinPrefix)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	// nothing should have been extracted on mismatch.
	assertAbsent(t, filepath.Join(dir, "buildkitd"))
}

func TestDownloadAndExtract_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := artifact{url: srv.URL, sha256: "deadbeef"}
	if err := downloadAndExtract(context.Background(), art, dir, stripBinPrefix); err == nil {
		t.Fatal("expected error on non-200 status")
	}
}

func TestPinsHaveBothArches(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		if a, ok := buildkitArtifacts[arch]; !ok || a.url == "" || len(a.sha256) != 64 {
			t.Errorf("buildkitArtifacts[%q] invalid: %+v", arch, a)
		}
		if a, ok := rootlesskitArtifacts[arch]; !ok || a.url == "" || len(a.sha256) != 64 {
			t.Errorf("rootlesskitArtifacts[%q] invalid: %+v", arch, a)
		}
	}
}
