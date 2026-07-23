// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFactStoreListActiveContainerImageSLSAFactsUsesActiveGenerations is the
// #5456 PR #5707 P1-b Postgres-side regression, mirroring
// TestFactStoreListActiveRepositoryFactsUsesActiveGenerations: the
// cross-scope SLSA/verification loader must resolve active-generation
// attestation.slsa_provenance facts regardless of which scope they were
// written in, so a container_image_identity refresh triggered from a
// different scope (the OCI registry) can still join them.
func TestFactStoreListActiveContainerImageSLSAFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-slsa-1",
					"sbom_attestation:supply-chain-demo:supply-chain-demo",
					"generation-1",
					"attestation.slsa_provenance",
					"sbom_attestation:supply-chain-demo:slsa-provenance:scd-attestation",
					"1.1.0",
					"sbom_attestation",
					int64(1),
					"reported",
					"sbom_attestation",
					"scd-attestation",
					"",
					"",
					time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"statement_id":"scd-attestation","predicate_type":"https://slsa.dev/provenance/v1"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageSLSAFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveContainerImageSLSAFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveContainerImageSLSAFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "attestation.slsa_provenance"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"'attestation.statement'",
		"'attestation.slsa_provenance'",
		"'attestation.signature_verification'",
		"fact.source_system = 'sbom_attestation'",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

// TestFactStoreListActiveContainerImageSLSAFactsExcludesTombstones mirrors
// TestFactStoreListActiveRepositoryFactsExcludesTombstones: a
// signature_verification fact tombstoned within a still-active generation
// (e.g. a superseded re-verification) must not be returned as live evidence
// the SLSA-tier gate could trust.
func TestFactStoreListActiveContainerImageSLSAFactsExcludesTombstones(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	if _, err := store.ListActiveContainerImageSLSAFacts(context.Background()); err != nil {
		t.Fatalf("ListActiveContainerImageSLSAFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "fact.is_tombstone = FALSE") {
		t.Fatalf("query must exclude tombstoned SLSA/verification facts via %q:\n%s",
			"fact.is_tombstone = FALSE", query)
	}
}
