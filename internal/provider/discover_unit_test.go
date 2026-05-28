// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":    true,
		"":             true,
		"127.0.0.1":    true,
		"::1":          true,
		"0.0.0.0":      false,
		"10.0.0.5":     false,
		"buildkitd":    false,
		"192.168.1.10": false,
	}
	for host, want := range cases {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}
