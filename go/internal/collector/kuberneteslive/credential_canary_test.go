// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// credentialSentinel is planted inside credential positions below and must
// never survive into a sanitized value or a persisted envelope.
const credentialSentinel = "EMBEDDED-CRED-SENTINEL-6119"

// TestSanitizeURLDropsOpaqueCredentials sweeps the sentinel through the
// userinfo spellings url.Parse does NOT surface as User: an opaque body
// (`svc:SECRET@host/x` — an operator omitting "https://" produces the same
// shape), a scheme-relative authority, and values net/url cannot parse at
// all. The old code returned those verbatim because its scheme/host guard
// read "not hierarchical" as "nothing to redact".
//
// The two wantKept rows are positive controls: a path-segment "@" and an
// opaque tool name are accepted on purpose, so the sweep cannot pass by
// returning "" for everything.
func TestSanitizeURLDropsOpaqueCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "opaque password position", value: "svc:" + credentialSentinel + "@h.internal:5432/tool"},
		{name: "opaque mid-password", value: "svc:pw" + credentialSentinel + "@h.internal/tool"},
		{name: "opaque under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/tool"},
		{name: "scheme-relative userinfo", value: "//svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "unparseable value", value: "https://svc:pw\"" + credentialSentinel + "@h.internal/tool"},
		{name: "unparseable only as an authority", value: "svc:pw]" + credentialSentinel + "@h.internal/tool"},
		{name: "hierarchical password stays redacted", value: "https://svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "path-segment at sign survives (positive control)", value: "https://h.internal/owners/" + credentialSentinel + "@example.com/x", wantKept: true},
		{name: "opaque tool name survives (positive control)", value: "mcp:tool/" + credentialSentinel, wantKept: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeURL(tt.value)
			if tt.wantKept {
				if !strings.Contains(got, credentialSentinel) {
					t.Fatalf("sanitizeURL(%q) = %q dropped a value that carries no credential; the sweep is over-redacting", tt.value, got)
				}
				return
			}
			if strings.Contains(got, credentialSentinel) {
				t.Errorf("sanitizeURL(%q) = %q kept the credential sentinel", tt.value, got)
			}
		})
	}
}

// TestWarningEnvelopeNeverPersistsOpaqueCredential drives the real envelope
// constructor with a credentialed opaque SourceURI and asserts the sentinel
// is absent from the marshaled envelope — the bytes that persist. The second
// run is the positive control: a path-segment sentinel must reach the same
// bytes, so the absence assertion is reading a real envelope.
func TestWarningEnvelopeNeverPersistsOpaqueCredential(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, sourceURI string) string {
		t.Helper()
		envelope, err := NewWarningEnvelope(WarningObservation{
			ClusterID:           "prod-us-east-1",
			Reason:              "list_pods_failed",
			ResourceScope:       "namespace/checkout",
			Message:             "listing pods failed",
			GenerationID:        "gen-1",
			CollectorInstanceID: "k8s-prod",
			FencingToken:        7,
			ObservedAt:          time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
			SourceURI:           sourceURI,
		})
		if err != nil {
			t.Fatalf("NewWarningEnvelope() error = %v, want nil", err)
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		return string(raw)
	}

	persisted := build(t, "svc:"+credentialSentinel+"@h.internal:6443/api")
	if strings.Contains(persisted, credentialSentinel) {
		t.Errorf("credential sentinel reached the persisted envelope:\n%s", persisted)
	}

	control := build(t, "https://h.internal/owners/"+credentialSentinel+"@example.com/api")
	if !strings.Contains(control, credentialSentinel) {
		t.Fatalf("positive control: path-segment sentinel missing from the marshaled envelope, so the absence assertion above is blind:\n%s", control)
	}
}
