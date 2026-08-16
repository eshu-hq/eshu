// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// credentialSentinel is planted inside credential positions below and must
// never survive into a sanitized URL or a persisted envelope.
const credentialSentinel = "EMBEDDED-CRED-SENTINEL-6119"

// TestStripSensitiveURLDropsOpaqueCredentials sweeps the sentinel through the
// userinfo spellings url.Parse does NOT surface as User. stripSensitiveURL
// tested parsed.User after a plain parse, and an opaque `svc:SECRET@host/x`
// (the same shape an operator produces by omitting "https://") parses with
// User == nil and the credential in Opaque — so the sanitizer re-emitted it
// verbatim into the fact's source_ref.
//
// The wantKept rows are positive controls: a path-segment "@" and a purl are
// kept on purpose, so the sweep cannot pass by returning "" for everything.
func TestStripSensitiveURLDropsOpaqueCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "opaque password position", value: "svc:" + credentialSentinel + "@h.internal:5432/catalog"},
		{name: "opaque mid-password", value: "svc:pw" + credentialSentinel + "@h.internal/catalog"},
		{name: "opaque under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/catalog"},
		{name: "scheme-relative userinfo", value: "//svc:" + credentialSentinel + "@h.internal/catalog"},
		{name: "unparseable value", value: "https://svc:pw\"" + credentialSentinel + "@h.internal/catalog"},
		{name: "unparseable only as an authority", value: "svc:pw]" + credentialSentinel + "@h.internal/catalog"},
		{name: "hierarchical userinfo stays dropped", value: "https://svc:" + credentialSentinel + "@h.internal/catalog"},
		{name: "path-segment at sign survives (positive control)", value: "https://h.internal/owners/" + credentialSentinel + "@example.com/x", wantKept: true},
		{name: "purl survives (positive control)", value: "pkg:npm/" + credentialSentinel + "@4.17.21", wantKept: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripSensitiveURL(tt.value)
			if tt.wantKept {
				if !strings.Contains(got, credentialSentinel) {
					t.Fatalf("stripSensitiveURL(%q) = %q dropped a value that carries no credential; the sweep is over-redacting", tt.value, got)
				}
				return
			}
			if strings.Contains(got, credentialSentinel) {
				t.Errorf("stripSensitiveURL(%q) = %q kept the credential sentinel", tt.value, got)
			}
		})
	}
}

// TestNewEnvelopeNeverPersistsOpaqueCredentialSourceURI drives the real
// envelope constructor with a credentialed opaque SourceURI and asserts the
// sentinel is absent from the marshaled envelope — the bytes that persist.
// The second run is the positive control: a path-segment sentinel must reach
// the same bytes, so the absence assertion is reading a real envelope.
func TestNewEnvelopeNeverPersistsOpaqueCredentialSourceURI(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, sourceURI string) string {
		t.Helper()
		envelope := newEnvelope(FixtureContext{
			ScopeID:             "service-catalog:example",
			GenerationID:        "generation-1",
			CollectorInstanceID: "fixture-catalog",
			FencingToken:        1,
			ObservedAt:          time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			SourceURI:           sourceURI,
		}, "service_catalog.service", "svc:example", "record-1", map[string]any{"name": "example"})
		out, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		return string(out)
	}

	persisted := build(t, "svc:"+credentialSentinel+"@h.internal:5432/catalog")
	if strings.Contains(persisted, credentialSentinel) {
		t.Errorf("credential sentinel reached the persisted envelope:\n%s", persisted)
	}

	control := build(t, "https://h.internal/owners/"+credentialSentinel+"@example.com/manifest.yaml")
	if !strings.Contains(control, credentialSentinel) {
		t.Fatalf("positive control: path-segment sentinel missing from the marshaled envelope, so the absence assertion above is blind:\n%s", control)
	}
}
