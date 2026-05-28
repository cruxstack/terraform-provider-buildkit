// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	cleanupOnce sync.Once
	cleanupMu   sync.Mutex
	cleanups    []func()
)

// registerProcessCleanup arranges for fn to run when the plugin process
// receives a termination signal. Multiple cleanups are supported; they run in
// reverse registration order. Used to reap supervised child processes such as
// an embedded buildkitd.
func registerProcessCleanup(fn func()) {
	if fn == nil {
		return
	}
	cleanupMu.Lock()
	cleanups = append(cleanups, fn)
	cleanupMu.Unlock()

	cleanupOnce.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-ch
			runCleanups()
			os.Exit(0)
		}()
	})
}

func runCleanups() {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i] != nil {
			cleanups[i]()
		}
	}
	cleanups = nil
}
