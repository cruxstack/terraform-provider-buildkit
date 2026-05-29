// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	bkclient "github.com/moby/buildkit/client"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider/buildkitbin"
)

// startEmbeddedBuildkitd provisions the `buildkitd` binary (downloading a
// pinned, checksum-verified release when necessary) and supervises it as a
// child process for the lifetime of the provider, exposing it on a private unix
// socket. This mirrors the approach of buildctl-daemonless.sh rather than
// embedding the daemon in-process (which would require bundling runc/rootlesskit
// and managing subuid/subgid, /proc, and snapshotter setup).
//
// Binaries are resolved via the buildkitbin package, which prefers (in order) an
// explicit BUILDKIT_EMBEDDED_BIN_DIR, a provider-managed cache, the host PATH,
// and finally a download of the pinned release. For unprivileged use,
// `rootlesskit` is also provisioned and configured /etc/subuid + /etc/subgid
// entries are required on the host (see rootlesscontaine.rs).
func startEmbeddedBuildkitd(ctx context.Context, opts embeddedOptions) (*resolvedEndpoint, error) {
	rootless := os.Geteuid() != 0

	if rootless {
		if err := preflightRootless(); err != nil {
			return nil, err
		}
	}

	tools, err := buildkitbin.Ensure(ctx, buildkitbin.Options{
		NeedRootlesskit: rootless,
		AllowDownload:   opts.allowDownload,
	})
	if err != nil {
		return nil, err
	}
	buildkitd := tools.Buildkitd

	runDir, err := os.MkdirTemp(embeddedRuntimeRoot(), "buildkit-tf-")
	if err != nil {
		return nil, fmt.Errorf("creating embedded buildkit runtime dir: %w", err)
	}
	sock := filepath.Join(runDir, "buildkitd.sock")
	addr := "unix://" + sock
	root := filepath.Join(runDir, "data")

	var cmd *exec.Cmd
	args := []string{
		"--addr", addr,
		"--root", root,
	}
	if rootless {
		// no-process-sandbox avoids needing systempaths=unconfined; acceptable
		// because the daemon already runs unprivileged.
		args = append(args, "--oci-worker-no-process-sandbox")
		full := append([]string{buildkitd}, args...)
		cmd = exec.Command(tools.Rootlesskit, full...) //nolint:gosec // args are provider-controlled
	} else {
		cmd = exec.Command(buildkitd, args...) //nolint:gosec // args are provider-controlled
	}

	// prepend the provisioned bin dir to PATH so buildkitd (and rootlesskit) can
	// exec the bundled buildkit-runc and other helpers.
	cmd.Env = append(os.Environ(),
		"XDG_RUNTIME_DIR="+runDir,
		"HOME="+homeOrTemp(),
		"PATH="+tools.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	// Send daemon logs to a file inside runDir instead of sharing the provider's
	// own os.Stdout/os.Stderr with the daemon. For rootless, rootlesskit
	// re-execs buildkitd (and runc) into new namespaces and those grandchildren
	// inherit whatever descriptors are wired here. If they inherited the
	// provider's stderr, a long-lived/leaked daemon child would keep that
	// descriptor open after the provider (or `go test` harness) exits, causing
	// the parent to block on stdout/stderr EOF ("Test I/O incomplete ...
	// WaitDelay expired"). A dedicated log file avoids leaking the parent's
	// streams; the path is reported so logs remain discoverable.
	logPath := filepath.Join(runDir, "buildkitd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // path is provider-controlled inside runDir
	if err != nil {
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("creating embedded buildkitd log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// run the daemon (and, for rootless, its rootlesskit-reexeced children) in
	// its own process group so cleanup can signal the whole group and avoid
	// leaving orphaned buildkitd/runc processes if the provider exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("starting buildkitd: %w", err)
	}
	// The child holds its own dup of the log file descriptor; the parent no
	// longer needs it open.
	_ = logFile.Close()

	pgid := cmd.Process.Pid
	cleanup := func() {
		// signal the entire process group (negative pid) so rootlesskit's
		// grandchildren are reaped too, then fall back to killing the direct
		// child if the group signal did not apply.
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = os.RemoveAll(runDir)
	}

	// wait for the daemon to become reachable.
	client, err := waitForEmbedded(ctx, addr, 30*time.Second)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("waiting for embedded buildkitd: %w", err)
	}

	return &resolvedEndpoint{
		client:  client,
		source:  "embedded buildkitd (" + pidString(cmd) + ", " + addr + ", binaries: " + tools.Source + ")",
		cleanup: cleanup,
	}, nil
}

// preflightRootless checks the host prerequisites for running buildkitd as an
// unprivileged user and returns an actionable error when they are missing. It is
// intentionally permissive: it only fails for conditions that are known to make
// rootless buildkitd unusable, and otherwise lets the daemon attempt to start.
func preflightRootless() error {
	// rootless requires entries in /etc/subuid and /etc/subgid for the current
	// user (or an enclosing user namespace already providing the mapping).
	if !hasSubIDMapping() {
		user := currentUserName()
		return fmt.Errorf(
			"running as an unprivileged user (euid=%d) but no /etc/subuid or /etc/subgid "+
				"mapping was found for %q, which rootless buildkitd requires. "+
				"Add entries (e.g. `%s:100000:65536` to both files), run the provider as root "+
				"(e.g. inside a container), or set buildkit_address / BUILDKIT_HOST to an existing endpoint",
			os.Geteuid(), user, user,
		)
	}
	return nil
}

// hasSubIDMapping reports whether /etc/subuid and /etc/subgid contain an entry
// for the current user (by name or uid). If the files cannot be read at all we
// assume a managed/namespaced environment and do not block.
func hasSubIDMapping() bool {
	name := currentUserName()
	uid := strconv.Itoa(os.Getuid())
	uidMapped := subIDFileMentions("/etc/subuid", name, uid)
	gidMapped := subIDFileMentions("/etc/subgid", name, uid)
	return uidMapped && gidMapped
}

func subIDFileMentions(path, name, uid string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // fixed, well-known system path
	if err != nil {
		// Unreadable/absent: don't block; the daemon may run inside an existing
		// user namespace that already provides mappings.
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		owner := line
		if i := strings.IndexByte(line, ':'); i >= 0 {
			owner = line[:i]
		}
		if owner == name || owner == uid {
			return true
		}
	}
	return false
}

func currentUserName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("LOGNAME"); u != "" {
		return u
	}
	return strconv.Itoa(os.Getuid())
}

func waitForEmbedded(ctx context.Context, addr string, timeout time.Duration) (*bkclient.Client, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := bkclient.New(ctx, addr)
		if err == nil {
			if _, err := c.ListWorkers(ctx); err == nil {
				return c, nil
			} else {
				lastErr = err
				_ = c.Close()
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out")
	}
	return nil, lastErr
}

func embeddedRuntimeRoot() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return x
	}
	return os.TempDir()
}

func homeOrTemp() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.TempDir()
}

func pidString(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil {
		return "pid=?"
	}
	return "pid=" + strconv.Itoa(cmd.Process.Pid)
}
