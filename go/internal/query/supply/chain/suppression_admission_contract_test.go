// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"testing"

	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerabilitysuppression/v1"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestSuppressionAdmissionContractIsVersioned pins the #8872 admission-contract
// version for the single bounded in-API mutation (operator suppression
// upsert). A breaking change to the admission shape must bump the version in
// the same diff so the eshu-api extraction boundary stays explicit.
func TestSuppressionAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SuppressionAdmissionContractVersion != "v0" {
		t.Errorf("SuppressionAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SuppressionAdmissionContractVersion, "v0")
	}
}

// TestPostgresStoreSatisfiesAdmissionContract proves the core-owned Postgres
// mutation store behind the admission port: it assigns to SuppressionAdmission
// and fails closed (an error, never a panic) when its storage is absent, so
// the API handler depends on the versioned contract rather than a direct
// table handle.
func TestPostgresStoreSatisfiesAdmissionContract(t *testing.T) {
	t.Parallel()

	var admission SuppressionAdmission = &impact.PostgresVulnerabilitySuppressionMutationStore{}
	_, err := admission.UpsertVulnerabilitySuppression(context.Background(), vulnerabilitysuppressionv1.Suppression{})
	if err == nil {
		t.Fatal("UpsertVulnerabilitySuppression with absent storage = nil, want a fail-closed error")
	}
}
