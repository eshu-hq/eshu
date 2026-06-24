// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testIdentityObservation() IdentityObservation {
	return IdentityObservation{
		Boundary:        testBoundary(),
		ARMResourceID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01",
		IdentityType:    IdentityTypeSystemAssigned,
		PrincipalID:     "aaaaaaaa-1111-2222-3333-444444444444",
		TenantID:        "99999999-9999-9999-9999-999999999999",
		RoleClass:       "contributor",
		AssignmentScope: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app",
	}
}

// TestNewIdentityObservationEnvelopeFingerprintsPrincipals proves identity GUIDs
// are fingerprinted (never raw) while the identity type, role class, and scope
// stay as bounded evidence.
func TestNewIdentityObservationEnvelopeFingerprintsPrincipals(t *testing.T) {
	obs := testIdentityObservation()
	key := testRedactionKey(t)

	env, err := NewIdentityObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewIdentityObservationEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureIdentityObservationFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.SchemaVersion != facts.AzureIdentityObservationSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	principalFp, _ := env.Payload["principal_fingerprint"].(string)
	if principalFp == "" || principalFp == obs.PrincipalID {
		t.Fatalf("principal_fingerprint = %q, want a non-raw marker", principalFp)
	}
	tenantFp, _ := env.Payload["tenant_fingerprint"].(string)
	if tenantFp == "" || tenantFp == obs.TenantID {
		t.Fatalf("tenant_fingerprint = %q, want a non-raw marker", tenantFp)
	}
	// Raw GUIDs must never appear anywhere in the payload values.
	for k, v := range env.Payload {
		if s, ok := v.(string); ok && (s == obs.PrincipalID || s == obs.TenantID) {
			t.Fatalf("raw GUID leaked in payload[%q] = %q", k, s)
		}
	}
	if env.Payload["identity_type"] != IdentityTypeSystemAssigned {
		t.Fatalf("identity_type = %#v", env.Payload["identity_type"])
	}
	if env.Payload["role_class"] != "contributor" {
		t.Fatalf("role_class = %#v", env.Payload["role_class"])
	}
	// Optional GUIDs left blank produce no fingerprint key.
	if _, present := env.Payload["client_fingerprint"]; present {
		t.Fatalf("client_fingerprint present for blank client id: %#v", env.Payload["client_fingerprint"])
	}
}

// TestNewIdentityObservationEnvelopeKeyDependentDeterministic proves the GUID
// fingerprints are deterministic for one key and change with the key material.
func TestNewIdentityObservationEnvelopeKeyDependentDeterministic(t *testing.T) {
	obs := testIdentityObservation()
	key := testRedactionKey(t)

	a, err := NewIdentityObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := NewIdentityObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a.Payload["principal_fingerprint"] != b.Payload["principal_fingerprint"] {
		t.Fatal("principal fingerprint not deterministic for the same key")
	}
	other, err := redact.NewKey([]byte("a-different-azure-identity-key-entirely"))
	if err != nil {
		t.Fatalf("other key: %v", err)
	}
	c, err := NewIdentityObservationEnvelope(obs, other)
	if err != nil {
		t.Fatalf("c: %v", err)
	}
	if c.Payload["principal_fingerprint"] == a.Payload["principal_fingerprint"] {
		t.Fatal("principal fingerprint must depend on the redaction key")
	}
	// The stable key is independent of the redaction key, so key rotation does
	// not split the same identity row.
	if a.StableFactKey != c.StableFactKey {
		t.Fatalf("stable key changed with redaction key: %q vs %q", a.StableFactKey, c.StableFactKey)
	}
}

// TestNewIdentityObservationEnvelopeRejectsInvalid proves the builder fails
// closed on a missing resource id, an unknown identity type, or a zero key.
func TestNewIdentityObservationEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	for name, mutate := range map[string]func(*IdentityObservation){
		"missing arm":   func(o *IdentityObservation) { o.ARMResourceID = "" },
		"unknown type":  func(o *IdentityObservation) { o.IdentityType = "made-up" },
		"no principals": func(o *IdentityObservation) { o.PrincipalID = ""; o.ClientID = ""; o.ObjectID = ""; o.TenantID = "" },
	} {
		obs := testIdentityObservation()
		mutate(&obs)
		if _, err := NewIdentityObservationEnvelope(obs, key); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
	// Zero key must fail closed.
	if _, err := NewIdentityObservationEnvelope(testIdentityObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
