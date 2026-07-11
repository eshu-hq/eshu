// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "testing"

// TestIsSensitiveKeyNameAgreesWithValidatePayloadKeys proves the exported
// IsSensitiveKeyName predicate is the SAME rule validatePayloadKeys applies at
// validation.go:284, not a reimplementation that can drift from it. Every case
// here is duplicated against validatePayload so a future edit to the
// unexported walk and a future edit to the exported wrapper cannot silently
// diverge without failing this test.
func TestIsSensitiveKeyNameAgreesWithValidatePayloadKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		key         string
		wantRejects bool
	}{
		{name: "secret", key: "secret", wantRejects: true},
		{name: "password", key: "password", wantRejects: true},
		{name: "api_key", key: "api_key", wantRejects: true},
		{name: "api-key hyphen", key: "api-key", wantRejects: true},
		{name: "authorization", key: "authorization", wantRejects: true},
		{name: "credential", key: "credential", wantRejects: true},
		{name: "access_token substring", key: "access_token", wantRejects: true},
		{name: "case insensitive TOKEN", key: "AUTH_TOKEN", wantRejects: true},
		{name: "allowlisted token_policy_join_keys", key: "token_policy_join_keys", wantRejects: false},
		{name: "benign key", key: "repository_name", wantRejects: false},
		{name: "benign key scope_id", key: "scope_id", wantRejects: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsSensitiveKeyName(tt.key)
			if got != tt.wantRejects {
				t.Fatalf("IsSensitiveKeyName(%q) = %v, want %v", tt.key, got, tt.wantRejects)
			}

			// Cross-check against the underlying fail-closed walk: a payload
			// whose only key is tt.key must be rejected by validatePayload iff
			// IsSensitiveKeyName says so, proving the exported predicate and the
			// internal walk cannot disagree.
			err := validatePayload(map[string]any{tt.key: "value"})
			rejectedByWalk := err != nil
			if rejectedByWalk != tt.wantRejects {
				t.Fatalf("validatePayload({%q: ...}) rejected = %v, want %v (drift between validatePayloadKeys and IsSensitiveKeyName)", tt.key, rejectedByWalk, tt.wantRejects)
			}
		})
	}
}

// TestValidateShareSafeKeysWrapsValidatePayloadKeys proves ValidateShareSafeKeys
// is a thin, behavior-preserving wrapper over the same fail-closed recursive
// walk validatePayloadKeys performs, applied to an arbitrary decoded JSON
// document rather than only a collector Fact.Payload.
func TestValidateShareSafeKeysWrapsValidatePayloadKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		doc     any
		wantErr bool
	}{
		{
			name:    "clean nested document",
			doc:     map[string]any{"a": map[string]any{"scope_id": "s1", "values": []any{1, 2, 3}}},
			wantErr: false,
		},
		{
			name:    "sensitive key nested under array",
			doc:     map[string]any{"items": []any{map[string]any{"api_key": "shh"}}},
			wantErr: true,
		},
		{
			name:    "sensitive key at top level",
			doc:     map[string]any{"password": "shh"},
			wantErr: true,
		},
		{
			name:    "allowlisted key passes",
			doc:     map[string]any{"token_policy_join_keys": []any{"sha256:fingerprint"}},
			wantErr: false,
		},
		{
			name:    "non-map document",
			doc:     []any{"just", "a", "list"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateShareSafeKeys(tt.doc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateShareSafeKeys(%+v) error = %v, wantErr %v", tt.doc, err, tt.wantErr)
			}
		})
	}
}
