// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildkitbin

// This file pins the exact upstream release artifacts the provider will
// download and run for embedded BuildKit. Versions and checksums are pinned for
// reproducibility and supply-chain integrity: every download is verified
// against the SHA256 recorded here before it is extracted or executed.
//
// The SHA256 values are of the *tarball* as published on GitHub releases. They
// were computed by downloading the assets directly from the release URLs below.
// To bump a version: download the new tarball, run `shasum -a 256 <file>`, and
// update the corresponding entry.

// buildkitVersion is the moby/buildkit release that is downloaded. It should be
// kept in sync with the github.com/moby/buildkit client version in go.mod so the
// daemon and client speak a compatible protocol.
const buildkitVersion = "v0.29.0"

// rootlesskitVersion is the rootless-containers/rootlesskit release used to run
// buildkitd unprivileged.
const rootlesskitVersion = "v3.0.1"

// artifact describes a single downloadable tarball and how to verify it.
type artifact struct {
	url    string
	sha256 string
}

// buildkitArtifacts maps GOARCH -> the buildkit release tarball. These tarballs
// bundle buildkitd, buildkit-runc, the CNI plugins, and qemu emulators under a
// top-level bin/ directory.
var buildkitArtifacts = map[string]artifact{
	"amd64": {
		url:    "https://github.com/moby/buildkit/releases/download/" + buildkitVersion + "/buildkit-" + buildkitVersion + ".linux-amd64.tar.gz",
		sha256: "ab8d93c72253b450f34a43e1c480abc52380f4aec3a8a395aebf09489efef7a0",
	},
	"arm64": {
		url:    "https://github.com/moby/buildkit/releases/download/" + buildkitVersion + "/buildkit-" + buildkitVersion + ".linux-arm64.tar.gz",
		sha256: "99a279e30be2947294eece98d82d1461fcfdc47da59514cb85252bb5ef414801",
	},
}

// rootlesskitArtifacts maps GOARCH -> the rootlesskit release tarball. These
// tarballs contain rootlesskit, rootlessctl, and rootlesskit-docker-proxy at the
// archive root (no leading directory).
var rootlesskitArtifacts = map[string]artifact{
	"amd64": {
		url:    "https://github.com/rootless-containers/rootlesskit/releases/download/" + rootlesskitVersion + "/rootlesskit-x86_64.tar.gz",
		sha256: "0850aa446151dfbdca15ed228ff0151751792cb5a99260b9a6738e1b490cc37b",
	},
	"arm64": {
		url:    "https://github.com/rootless-containers/rootlesskit/releases/download/" + rootlesskitVersion + "/rootlesskit-aarch64.tar.gz",
		sha256: "fdd9d2aa12bb8914081dfe1cd129c5a6a06cf1f80c732713f905a92c07b9b45c",
	},
}
