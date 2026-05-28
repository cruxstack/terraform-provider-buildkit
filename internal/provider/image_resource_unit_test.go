// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFullRef(t *testing.T) {
	cases := []struct {
		registry, repository, tag, want string
	}{
		{"ghcr.io", "org/app", "latest", "ghcr.io/org/app:latest"},
		{"https://ghcr.io/", "org/app", "v1", "ghcr.io/org/app:v1"},
		{"http://127.0.0.1:5000", "x/y", "t", "127.0.0.1:5000/x/y:t"},
	}
	for _, c := range cases {
		if got := fullRef(c.registry, c.repository, c.tag); got != c.want {
			t.Errorf("fullRef(%q,%q,%q) = %q, want %q", c.registry, c.repository, c.tag, got, c.want)
		}
	}
}

func pub(push, insecure bool) publishModel {
	return publishModel{Push: types.BoolValue(push), Insecure: types.BoolValue(insecure)}
}

func TestUniformPublishFlags(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		push, insecure, err := uniformPublishFlags(nil)
		if err != nil || push || insecure {
			t.Fatalf("got push=%v insecure=%v err=%v", push, insecure, err)
		}
	})
	t.Run("uniform", func(t *testing.T) {
		push, insecure, err := uniformPublishFlags([]publishModel{pub(true, true), pub(true, true)})
		if err != nil || !push || !insecure {
			t.Fatalf("got push=%v insecure=%v err=%v", push, insecure, err)
		}
	})
	t.Run("mixed push", func(t *testing.T) {
		if _, _, err := uniformPublishFlags([]publishModel{pub(true, false), pub(false, false)}); err == nil {
			t.Fatal("expected error for mixed push")
		}
	})
	t.Run("mixed insecure", func(t *testing.T) {
		if _, _, err := uniformPublishFlags([]publishModel{pub(true, false), pub(true, true)}); err == nil {
			t.Fatal("expected error for mixed insecure")
		}
	})
}

func TestParseDigestRef(t *testing.T) {
	const dig = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	t.Run("valid", func(t *testing.T) {
		reg, repo, d, err := parseDigestRef("ghcr.io/org/app@" + dig)
		if err != nil {
			t.Fatal(err)
		}
		if reg != "ghcr.io" || repo != "org/app" || d != dig {
			t.Fatalf("got %q %q %q", reg, repo, d)
		}
	})
	t.Run("scheme stripped", func(t *testing.T) {
		reg, repo, _, err := parseDigestRef("https://ghcr.io/org/app@" + dig)
		if err != nil || reg != "ghcr.io" || repo != "org/app" {
			t.Fatalf("got %q %q err=%v", reg, repo, err)
		}
	})
	t.Run("missing digest", func(t *testing.T) {
		if _, _, _, err := parseDigestRef("ghcr.io/org/app:latest"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("bad digest", func(t *testing.T) {
		if _, _, _, err := parseDigestRef("ghcr.io/org/app@sha256:short"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing repository", func(t *testing.T) {
		if _, _, _, err := parseDigestRef("ghcr.io@" + dig); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMergeSecrets(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	plain, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{
		"token": "plain-value",
	})
	b64, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{
		// base64("decoded")
		"enc": "ZGVjb2RlZA==",
	})

	out, _ := mergeSecrets(ctx, plain, b64, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if string(out["token"]) != "plain-value" {
		t.Errorf("plain secret = %q", out["token"])
	}
	if string(out["enc"]) != "decoded" {
		t.Errorf("decoded secret = %q", out["enc"])
	}
}

func TestMergeSecretsInvalidBase64(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	b64, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{
		"bad": "not!base64!",
	})
	_, _ = mergeSecrets(ctx, types.MapNull(types.StringType), b64, &diags)
	if !diags.HasError() {
		t.Fatal("expected a diagnostic for invalid base64")
	}
}
