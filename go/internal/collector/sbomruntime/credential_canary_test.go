// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"strings"
	"testing"
)

// credentialSentinel is planted inside credential positions below and must
// never survive into a source URI.
const credentialSentinel = "EMBEDDED-CRED-SENTINEL-6119"

// TestSafeSourceURIDropsOpaqueCredentials sweeps the sentinel through the
// userinfo spellings url.Parse does NOT surface as User. The old guard was
// Scheme == "", but an opaque `svc:SECRET@host/x` HAS a scheme, so the value
// re-strung through url.URL.String() with the credential intact. safeSourceURI
// feeds SourceRef.SourceURI on every envelope this package emits
// (source.go), so a kept sentinel here is a persisted one.
//
// The wantKept rows are positive controls: a hierarchical URI and a purl
// (whose "@" sits after the first "/") must keep flowing, so the sweep cannot
// pass by returning "" for everything.
func TestSafeSourceURIDropsOpaqueCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "opaque password position", value: "svc:" + credentialSentinel + "@h.internal:5432/tool"},
		{name: "opaque mid-password", value: "svc:pw" + credentialSentinel + "@h.internal/tool"},
		{name: "opaque under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/tool"},
		{name: "unparseable only as an authority", value: "svc:pw]" + credentialSentinel + "@h.internal/tool"},
		{name: "hierarchical password stays redacted", value: "https://svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "hierarchical path survives (positive control)", value: "https://h.internal/sbom/" + credentialSentinel + "@example.com/doc", wantKept: true},
		{name: "purl survives (positive control)", value: "pkg:npm/" + credentialSentinel + "@4.17.21", wantKept: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := safeSourceURI(tt.value)
			if tt.wantKept {
				if !strings.Contains(got, credentialSentinel) {
					t.Fatalf("safeSourceURI(%q) = %q dropped a value that carries no credential; the sweep is over-redacting", tt.value, got)
				}
				return
			}
			if strings.Contains(got, credentialSentinel) {
				t.Errorf("safeSourceURI(%q) = %q kept the credential sentinel", tt.value, got)
			}
		})
	}
}
