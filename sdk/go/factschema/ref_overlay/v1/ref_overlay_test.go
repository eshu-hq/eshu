// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"reflect"
	"strings"
	"testing"
)

// TestStripURLUserinfo proves the mandatory pre-emission normalization for
// the config_key_divergence contract (issue #5393): a URL-shaped value is
// stripped of its userinfo (credentials) before anything derived from it is
// emitted, so a divergence row can never carry a secret in a key, a value
// kind classification, or any future redacted field.
func TestStripURLUserinfo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "userinfo with password is stripped",
			raw:  "https://deploy:s3cret@example.com:8443/app?env=prod#frag",
			want: "https://example.com:8443/app?env=prod#frag",
		},
		{
			name: "bare username is stripped",
			raw:  "https://deploy@example.com/app",
			want: "https://example.com/app",
		},
		{
			name: "no userinfo passes through unchanged",
			raw:  "https://example.com/app?env=prod",
			want: "https://example.com/app?env=prod",
		},
		{
			name: "non-url value passes through unchanged",
			raw:  "us-east-1",
			want: "us-east-1",
		},
		{
			name: "empty passes through unchanged",
			raw:  "",
			want: "",
		},
		{
			name: "unparseable with at-sign passes through unchanged",
			raw:  "://[::1",
			want: "://[::1",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := StripURLUserinfo(tc.raw); got != tc.want {
				t.Fatalf("StripURLUserinfo(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestConfigKeyDivergenceCarriesNoValuesOrHashes pins the "keys + per-env
// divergence booleans only" bound from the #5393 isolation contract as an
// exact field set: any future field carrying a value, hash, or secret fails
// here, so a divergence answer stays structurally incapable of leaking one.
// value_kind is the deliberate exception — a shape-class tag (url, string,
// ...) recorded without the value itself, classified from the
// userinfo-stripped form.
func TestConfigKeyDivergenceCarriesNoValuesOrHashes(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"collector_instance_id": true,
		"repo_id":               true,
		"ref":                   true,
		"config_key":            true,
		"env":                   true,
		"diverged":              true,
		"value_kind":            true,
	}
	typ := reflect.TypeOf(ConfigKeyDivergence{})
	got := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := field.Tag.Get("json")
		if idx := strings.Index(name, ","); idx >= 0 {
			name = name[:idx]
		}
		got[name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfigKeyDivergence fields = %v, want exactly %v: divergence facts carry keys and booleans only", got, want)
	}
}

// TestRefKindAndResolvabilityClosedSets pins the documented closed value
// sets: every constant is distinct and non-empty so emitters and readers
// share one vocabulary.
func TestRefKindAndResolvabilityClosedSets(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	for _, tc := range []struct {
		goName string
		value  string
	}{
		{"RefKindBranch", RefKindBranch},
		{"RefKindTag", RefKindTag},
		{"RefKindCommit", RefKindCommit},
		{"ResolvabilityPinned", ResolvabilityPinned},
		{"ResolvabilityMobile", ResolvabilityMobile},
		{"ResolvabilityRange", ResolvabilityRange},
		{"ResolvabilityUnresolved", ResolvabilityUnresolved},
	} {
		if tc.value == "" {
			t.Fatalf("%s is empty", tc.goName)
		}
		if prior, dup := seen[tc.value]; dup {
			t.Fatalf("%s duplicates %s with value %q", tc.goName, prior, tc.value)
		}
		seen[tc.value] = tc.goName
	}
}
