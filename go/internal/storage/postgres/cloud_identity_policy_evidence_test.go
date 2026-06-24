// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestPostgresCloudIdentityPolicyEvidenceLoaderMapsAzureIdentityFacts proves
// the loader reads only bounded Azure identity-policy fields and keyed
// fingerprints for one scope generation. Raw principal GUIDs and assignment
// scopes must not leave the source-fact payload.
func TestPostgresCloudIdentityPolicyEvidenceLoaderMapsAzureIdentityFacts(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "azure:tenant:subscription:sub-1:all:all:resource_graph"
		generationID = "gen-identity-1"
		armID        = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{
					facts.AzureIdentityObservationFactKind,
					armID,
					"identity-stable-1",
					[]byte(`{
						"arm_resource_id":"` + armID + `",
						"identity_type":"system_assigned",
						"role_class":"contributor",
						"assignment_scope":"/subscriptions/sub-1/providers/Microsoft.Authorization/roleAssignments/raw-scope-marker",
						"principal_fingerprint":"principal-marker",
						"tenant_fingerprint":"tenant-marker"
					}`),
				},
				{
					facts.AzureIdentityObservationFactKind,
					"not-an-arm-id",
					"identity-stable-ambiguous",
					[]byte(`{
						"arm_resource_id":"not-an-arm-id",
						"identity_type":"system_assigned",
						"principal_fingerprint":"bad-marker"
					}`),
				},
			}},
		},
	}

	loader := PostgresCloudIdentityPolicyEvidenceLoader{DB: db}
	records, err := loader.LoadCloudIdentityPolicyEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudIdentityPolicyEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d (ambiguous ARM identity dropped)", got, want)
	}
	record := records[0]
	if record.Provider != cloudinventory.ProviderAzure {
		t.Fatalf("provider = %q, want %q", record.Provider, cloudinventory.ProviderAzure)
	}
	if record.RawIdentity != armID {
		t.Fatalf("raw identity = %q, want %q", record.RawIdentity, armID)
	}
	if record.IdentityType != "system_assigned" || record.RoleClass != "contributor" {
		t.Fatalf("bounded fields = (%q,%q), want system_assigned/contributor", record.IdentityType, record.RoleClass)
	}
	if record.PrincipalFingerprint != "principal-marker" || record.TenantFingerprint != "tenant-marker" {
		t.Fatalf("fingerprints = %#v", record)
	}
	if strings.Contains(fmt.Sprintf("%#v", record), "raw-scope-marker") {
		t.Fatalf("raw assignment scope leaked into loader record: %#v", record)
	}
}

// TestCloudIdentityPolicyEvidenceQueryIsBounded proves the SQL allowlist stays
// scoped to azure_identity_observation rows from one non-tombstoned generation.
func TestCloudIdentityPolicyEvidenceQueryIsBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"azure_identity_observation",
		"fact.scope_id = $1",
		"fact.generation_id = $2",
		"fact.is_tombstone = FALSE",
		"arm_resource_id",
	} {
		if !strings.Contains(listCloudIdentityPolicyEvidenceForGenerationQuery, want) {
			t.Fatalf("listCloudIdentityPolicyEvidenceForGenerationQuery missing %q", want)
		}
	}
}

// TestPostgresCloudIdentityPolicyEvidenceLoaderRequiresScopeAndGeneration
// proves blank selectors are rejected instead of widening the fact scan.
func TestPostgresCloudIdentityPolicyEvidenceLoaderRequiresScopeAndGeneration(t *testing.T) {
	t.Parallel()

	loader := PostgresCloudIdentityPolicyEvidenceLoader{DB: &fakeExecQueryer{}}
	if _, err := loader.LoadCloudIdentityPolicyEvidence(context.Background(), "", "gen-1"); err == nil {
		t.Fatal("blank scope: error = nil, want non-nil")
	}
	if _, err := loader.LoadCloudIdentityPolicyEvidence(context.Background(), "scope-1", ""); err == nil {
		t.Fatal("blank generation: error = nil, want non-nil")
	}
}

// TestBoundedIdentityPolicyStringCapsCopiedFields proves source payload text
// cannot make reducer read-model rows grow without a fixed field bound.
func TestBoundedIdentityPolicyStringCapsCopiedFields(t *testing.T) {
	t.Parallel()

	got := boundedIdentityPolicyString(strings.Repeat("a", maxCloudIdentityPolicyEvidenceFieldLength+1))
	if len(got) != maxCloudIdentityPolicyEvidenceFieldLength {
		t.Fatalf("boundedIdentityPolicyString length = %d, want %d", len(got), maxCloudIdentityPolicyEvidenceFieldLength)
	}
}
