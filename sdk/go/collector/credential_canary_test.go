// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

// credentialSentinel is planted inside credential positions below. A URI
// carrying it must be refused, and no error text may repeat it: these
// messages reach collector logs.
const credentialSentinel = "EMBEDDED-CRED-SENTINEL-6119"

// TestValidateResultRefusesOpaqueCredentialedSourceURI sweeps the sentinel
// through the userinfo spellings url.Parse does NOT surface as User. The old
// validateSourceURI tested parsed.User after a plain parse, and an opaque
// `svc:SECRET@host/x` (the same shape an operator produces by omitting
// "https://") parses with User == nil — so the contract gate certified a
// credentialed source_ref.uri as credential-free and let it persist.
//
// The wantAccepted rows are positive controls: a path-segment "@" and a purl
// are accepted on purpose, so the sweep cannot pass by refusing everything.
func TestValidateResultRefusesOpaqueCredentialedSourceURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uri          string
		wantAccepted bool
	}{
		{name: "opaque password position", uri: "svc:" + credentialSentinel + "@h.internal:5432/tool"},
		{name: "opaque mid-password", uri: "svc:pw" + credentialSentinel + "@h.internal/tool"},
		{name: "opaque under an uppercase scheme", uri: "SVC:" + credentialSentinel + "@h.internal/tool"},
		{name: "scheme-relative userinfo", uri: "//svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "hierarchical userinfo", uri: "https://svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "unparseable value", uri: "https://svc:pw\"" + credentialSentinel + "@h.internal/tool"},
		{name: "unparseable only as an authority", uri: "svc:pw]" + credentialSentinel + "@h.internal/tool"},
		{name: "path-segment at sign accepted (positive control)", uri: "https://h.internal/owners/" + credentialSentinel + "@example.com/x", wantAccepted: true},
		{name: "purl accepted (positive control)", uri: "pkg:npm/" + credentialSentinel + "@4.17.21", wantAccepted: true},
	}
	validator := NewValidator(testContract())
	base := readFixture(t, "complete")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			candidate := cloneResult(base)
			candidate.Facts[0].SourceRef.URI = tt.uri
			_, err := validator.ValidateResult(candidate)
			if tt.wantAccepted {
				if err != nil {
					t.Fatalf("ValidateResult() error = %v, want nil for a URI that carries no credential", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateResult() error = nil, want a refusal for %q", tt.uri)
			}
			if strings.Contains(err.Error(), credentialSentinel) {
				t.Errorf("ValidateResult() error %q repeats the credential; these messages reach collector logs", err)
			}
		})
	}
}
