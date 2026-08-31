// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

// This file holds the #5461/#5816 WindowFactCount authorization regression
// tests, split out of package_registry_scoped_access_test.go to keep that
// file under the repo's 500-line cap. See
// package_registry_correlation_page.go's PackageRegistryCorrelationPage.WindowFactCount
// doc comment and package_registry_scoped_access.go's
// packageRegistryGateForVisibility / packageRegistryGateForVisibilityBatch
// for the production code these tests pin.

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// candidateFactPackageRegistryCorrelationStore is a PackageRegistryCorrelationStore
// fake that stores each candidate package's RAW (undecoded) correlation facts
// and routes every call -- scalar (filter.PackageID set) or batched
// (filter.PackageIDs set) -- through the real buildPackageRegistryCorrelationPage
// pagination/decode logic, instead of a hand-rolled Rows slice. That matters
// here specifically: the regression these tests pin only exists when a fact
// is genuinely present but fails typed decode, which only
// buildPackageRegistryCorrelationPage's actual drop path can produce.
// Batched facts are concatenated in filter.PackageIDs order, mirroring how a
// real fact_id-ordered SQL fetch interleaves multiple candidates' facts
// within one shared LIMIT window.
type candidateFactPackageRegistryCorrelationStore struct {
	factsByPackageID map[string][]packageRegistryCorrelationFactRow
	calls            []PackageRegistryCorrelationFilter
}

func (s *candidateFactPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) (PackageRegistryCorrelationPage, error) {
	s.calls = append(s.calls, filter)
	var facts []packageRegistryCorrelationFactRow
	if filter.PackageID != "" {
		facts = s.factsByPackageID[filter.PackageID]
	} else {
		for _, id := range filter.PackageIDs {
			facts = append(facts, s.factsByPackageID[id]...)
		}
	}
	return buildPackageRegistryCorrelationPage(facts, filter.Limit+1)
}

// TestPackageRegistryGateForVisibilityGrantsWhenOnlyCorrelationFactFailsDecode
// is the regression test for the #5461/#5816 finding: packageRegistryGateForVisibility
// used to compute `granted := len(page.Rows) > 0`, the POST-decode row count.
// A correlation fact that genuinely exists in Postgres but fails typed decode
// (an unsupported schema major, or any other classified decode error) is
// silently dropped by buildPackageRegistryCorrelationPage's `continue`, so a
// caller whose only matching fact happens to be undecodable was silently
// DENIED -- where pre-#5461 main's hard-error-on-any-decode-failure behavior
// would have surfaced the problem loudly instead of quietly denying access.
// The fix reads page.WindowFactCount (the RAW pre-decode fact count in the
// visible window) instead of len(Rows), so evidence PRESENCE, not decode
// success, drives the grant.
func TestPackageRegistryGateForVisibilityGrantsWhenOnlyCorrelationFactFailsDecode(t *testing.T) {
	t.Parallel()

	const packageID = "pkg:npm:undecodable-only"
	correlations := &candidateFactPackageRegistryCorrelationStore{
		factsByPackageID: map[string][]packageRegistryCorrelationFactRow{
			packageID: {
				{
					FactID:        "fact-undecodable",
					FactKind:      packageConsumptionCorrelationFactKind,
					SchemaVersion: "2.0.0", // unsupported major: dead-letters, never a hard error
					Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
						"package_id":        packageID,
						"relationship_kind": "consumption",
						"repository_id":     "repo-a",
					}),
				},
			},
		},
	}
	access := querycontract.RepositoryAccessFilterFromContext(queryauth.ContextWithAuthContext(context.Background(), tenantAScopedAuthContext()))
	span := trace.SpanFromContext(context.Background())

	gate, err := packageRegistryGateForVisibility(context.Background(), span, correlations, packageID, "private", access)
	if err != nil {
		t.Fatalf("packageRegistryGateForVisibility: %v", err)
	}
	if !gate.proceed {
		t.Fatal("proceed = false, want true: the fact exists (raw fetch count 1) even though its typed decode failed -- WindowFactCount must gate this, not len(Rows)")
	}
}

// TestPackageRegistryGateForVisibilityBatchReverifiesCandidateWhoseOnlyFactFailsDecode
// pins the second ambiguous disjunct the #5461/#5816 fix adds:
// page.WindowFactCount > len(page.Rows) means at least one fact inside the
// batched window failed decode, and a decode-dropped fact carries no
// PackageID, so its absence from grantedSeen is never proof the candidate
// lacks a grant (unlike a genuinely empty result, where the store would have
// returned zero raw facts). Below packageRegistryMaxLimit the OLD ambiguous
// test (len(page.Rows) >= packageRegistryMaxLimit) missed this entirely: 1
// decoded row out of a 2-candidate/2-fact batch is nowhere near the cap, so
// the victim candidate was silently denied even though its only fact does
// exist. The individual scalar re-verify (same fake, keyed on
// filter.PackageID) must recover it.
func TestPackageRegistryGateForVisibilityBatchReverifiesCandidateWhoseOnlyFactFailsDecode(t *testing.T) {
	t.Parallel()

	const (
		pkgGranted = "pkg:npm:batch-granted"
		pkgVictim  = "pkg:npm:batch-victim"
	)
	correlations := &candidateFactPackageRegistryCorrelationStore{
		factsByPackageID: map[string][]packageRegistryCorrelationFactRow{
			pkgGranted: {
				{
					FactID:        "fact-granted",
					FactKind:      packageConsumptionCorrelationFactKind,
					SchemaVersion: "1.0.0",
					Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
						"package_id":        pkgGranted,
						"relationship_kind": "consumption",
						"repository_id":     "repo-a",
					}),
				},
			},
			pkgVictim: {
				{
					FactID:        "fact-victim-undecodable",
					FactKind:      packageConsumptionCorrelationFactKind,
					SchemaVersion: "2.0.0",
					Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
						"package_id":        pkgVictim,
						"relationship_kind": "consumption",
						"repository_id":     "repo-a",
					}),
				},
			},
		},
	}
	candidates := []packageRegistryNameCandidate{
		{PackageID: pkgGranted, Visibility: "private"},
		{PackageID: pkgVictim, Visibility: "private"},
	}
	access := querycontract.RepositoryAccessFilterFromContext(queryauth.ContextWithAuthContext(context.Background(), tenantAScopedAuthContext()))
	span := trace.SpanFromContext(context.Background())

	gates, err := packageRegistryGateForVisibilityBatch(context.Background(), span, correlations, candidates, access)
	if err != nil {
		t.Fatalf("packageRegistryGateForVisibilityBatch: %v", err)
	}
	if !gates[pkgGranted].proceed {
		t.Fatalf("gates[%q].proceed = false, want true", pkgGranted)
	}
	if !gates[pkgVictim].proceed {
		t.Fatalf("gates[%q].proceed = false, want true: WindowFactCount(2) > len(Rows)(1) must mark the batch ambiguous and trigger an individual re-verify that finds the victim's raw fact", pkgVictim)
	}
	if got, want := len(correlations.calls), 2; got != want {
		t.Fatalf("correlation store calls = %d, want %d (1 batch call + 1 individual re-verify for the victim)", got, want)
	}
}

// TestPackageRegistryGateForVisibilityBatchAtCapWithDecodeDropStillReverifies
// is the full-window variant pinning the `>= packageRegistryMaxLimit`
// ambiguous disjunct itself: a crowding candidate supplies
// packageRegistryMaxLimit-1 decodable facts and the probed candidate supplies
// exactly one undecodable fact, filling the raw window to precisely the cap.
// The OLD ambiguous test (len(page.Rows) >= packageRegistryMaxLimit) missed
// this: only packageRegistryMaxLimit-1 rows decoded (one short of the cap),
// so the crowded, decode-dropped candidate was silently denied even though
// the raw fetch genuinely hit the cap -- the same crowd-out class of bug this
// function exists to close (see its doc comment), now combined with a decode
// drop instead of a clean crowd-out. WindowFactCount reads the raw fetch
// count directly, so it still recognizes the cap and re-verifies.
func TestPackageRegistryGateForVisibilityBatchAtCapWithDecodeDropStillReverifies(t *testing.T) {
	t.Parallel()

	const (
		pkgCrowder = "pkg:npm:cap-crowder"
		pkgVictim  = "pkg:npm:cap-victim"
	)
	crowderFacts := make([]packageRegistryCorrelationFactRow, packageRegistryMaxLimit-1)
	for i := range crowderFacts {
		crowderFacts[i] = packageRegistryCorrelationFactRow{
			FactID:        fmt.Sprintf("fact-crowder-%d", i),
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        pkgCrowder,
				"relationship_kind": "consumption",
				"repository_id":     "repo-a",
			}),
		}
	}
	correlations := &candidateFactPackageRegistryCorrelationStore{
		factsByPackageID: map[string][]packageRegistryCorrelationFactRow{
			pkgCrowder: crowderFacts,
			pkgVictim: {
				{
					FactID:        "fact-victim-undecodable",
					FactKind:      packageConsumptionCorrelationFactKind,
					SchemaVersion: "2.0.0",
					Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
						"package_id":        pkgVictim,
						"relationship_kind": "consumption",
						"repository_id":     "repo-a",
					}),
				},
			},
		},
	}
	candidates := []packageRegistryNameCandidate{
		{PackageID: pkgCrowder, Visibility: "private"},
		{PackageID: pkgVictim, Visibility: "private"},
	}
	access := querycontract.RepositoryAccessFilterFromContext(queryauth.ContextWithAuthContext(context.Background(), tenantAScopedAuthContext()))
	span := trace.SpanFromContext(context.Background())

	gates, err := packageRegistryGateForVisibilityBatch(context.Background(), span, correlations, candidates, access)
	if err != nil {
		t.Fatalf("packageRegistryGateForVisibilityBatch: %v", err)
	}
	if !gates[pkgVictim].proceed {
		t.Fatalf("gates[%q].proceed = false, want true: a window at exactly packageRegistryMaxLimit raw facts must be ambiguous even when the decoded row count (packageRegistryMaxLimit-1) falls one short of the cap", pkgVictim)
	}
}
