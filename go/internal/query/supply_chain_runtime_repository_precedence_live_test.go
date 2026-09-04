// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

const (
	runtimePrecedenceRawRepository      = "github.com/example/repo-a"
	runtimePrecedenceRelatedRepository  = "repository:r_5747_related"
	runtimePrecedenceEnvelopeRepository = "github.com/example/repo-envelope"
	runtimePrecedenceEnvelopeGeneration = "generation:5747:raw-envelope"
	runtimePrecedenceDecoyPackage       = "pkg:deb/example/decoder-decoy"
	runtimePrecedenceWhitespaceRepo     = "repository:r_5747_whitespace"
	runtimePrecedenceScalarRepo         = "repository:r_5747_scalar"
	runtimePrecedenceLaterRepo          = "repository:r_5747_later"
)

type runtimeRepositoryPrecedenceCase struct {
	name         string
	repositoryID string
	packageID    string
	workloadID   string
	serviceID    string
	environment  string
}

func assertSupplyChainRuntimeRepositoryPrecedenceLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresSupplyChainImpactFindingStore,
	aggregateStore impact.PostgresSupplyChainImpactAggregateStore,
) {
	t.Helper()
	for _, tc := range []runtimeRepositoryPrecedenceCase{
		{
			name:         "raw_payload_scope",
			repositoryID: runtimePrecedenceRawRepository,
			packageID:    "pkg:deb/example/raw-payload",
			workloadID:   "workload:5747:raw-payload",
		},
		{
			name:         "related_scope_before_raw_fallback",
			repositoryID: runtimePrecedenceRelatedRepository,
			packageID:    "pkg:deb/example/related-scope",
			serviceID:    "service:5747:related-scope",
		},
		{
			name:         "raw_envelope_fallback",
			repositoryID: runtimePrecedenceEnvelopeRepository,
			packageID:    "pkg:deb/example/raw-envelope",
			environment:  "raw-envelope-5747",
		},
		{
			name:         "whitespace_related_scope",
			repositoryID: runtimePrecedenceWhitespaceRepo,
			packageID:    "pkg:deb/example/whitespace-related",
			workloadID:   "workload:5747:whitespace-related",
		},
		{
			name:         "scalar_related_scope",
			repositoryID: runtimePrecedenceScalarRepo,
			packageID:    "pkg:deb/example/scalar-related",
			serviceID:    "service:5747:scalar-related",
		},
		{
			name:         "malformed_then_valid_related_scope",
			repositoryID: runtimePrecedenceLaterRepo,
			packageID:    "pkg:deb/example/later-related",
			workloadID:   "workload:5747:later-related",
		},
	} {
		tc := tc
		t.Run("repository_precedence_"+tc.name, func(t *testing.T) {
			t.Run("canonical_hydration", func(t *testing.T) {
				assertRuntimeRepositoryPrecedenceHydration(t, ctx, findingStore, tc, tc.repositoryID, true)
			})
			t.Run("canonical_filters", func(t *testing.T) {
				assertRuntimeRepositoryPrecedenceFilter(
					t,
					ctx,
					findingStore,
					aggregateStore,
					tc,
					tc.repositoryID,
					tc.packageID,
					1,
				)
			})

			if tc.repositoryID == runtimePrecedenceEnvelopeRepository {
				return
			}
			t.Run("decoy_hydration", func(t *testing.T) {
				assertRuntimeRepositoryPrecedenceHydration(
					t,
					ctx,
					findingStore,
					tc,
					runtimeFilterLiveDecoyRepo,
					false,
				)
			})
			t.Run("decoy_filters", func(t *testing.T) {
				assertRuntimeRepositoryPrecedenceFilter(
					t,
					ctx,
					findingStore,
					aggregateStore,
					tc,
					runtimeFilterLiveDecoyRepo,
					runtimePrecedenceDecoyPackage,
					0,
				)
			})
		})
	}
}

func assertRuntimeRepositoryPrecedenceHydration(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
	tc runtimeRepositoryPrecedenceCase,
	repositoryID string,
	wantSelector bool,
) {
	t.Helper()
	handler := &SupplyChainHandler{ImpactFindings: store}
	rows := []impact.SupplyChainImpactFindingRow{{RepositoryID: repositoryID}}
	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{repositoryID}}
	if err := handler.applySupplyChainRuntimeContext(ctx, rows, access); err != nil {
		t.Fatalf("hydrate %s for %s: %v", tc.name, repositoryID, err)
	}
	resolved := rows[0].RuntimeContext
	if resolved == nil {
		t.Fatalf("hydrate %s for %s returned nil context", tc.name, repositoryID)
	}
	hasSelector := (tc.workloadID != "" && containsAuthString(resolved.WorkloadIDs, tc.workloadID)) ||
		(tc.serviceID != "" && containsAuthString(resolved.ServiceIDs, tc.serviceID)) ||
		(tc.environment != "" && containsAuthString(resolved.Environments, tc.environment))
	if hasSelector != wantSelector {
		t.Fatalf(
			"hydrate %s for %s selector present = %v, want %v: %+v",
			tc.name,
			repositoryID,
			hasSelector,
			wantSelector,
			resolved,
		)
	}
}

func assertRuntimeRepositoryPrecedenceFilter(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresSupplyChainImpactFindingStore,
	aggregateStore impact.PostgresSupplyChainImpactAggregateStore,
	tc runtimeRepositoryPrecedenceCase,
	repositoryID string,
	packageID string,
	want int,
) {
	t.Helper()
	listFilter := impact.SupplyChainImpactFindingFilter{
		CVEID:                runtimeFilterLiveCVE,
		PackageID:            packageID,
		WorkloadID:           tc.workloadID,
		ServiceID:            tc.serviceID,
		Environment:          tc.environment,
		DetectionProfile:     "comprehensive",
		Limit:                10,
		AllowedRepositoryIDs: []string{repositoryID},
	}
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, false, want)
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, true, want)

	aggregateFilter := impact.SupplyChainImpactAggregateFilter{
		CVEID:                runtimeFilterLiveCVE,
		PackageID:            packageID,
		WorkloadID:           tc.workloadID,
		ServiceID:            tc.serviceID,
		Environment:          tc.environment,
		DetectionProfile:     "comprehensive",
		AllowedRepositoryIDs: []string{repositoryID},
	}
	count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, aggregateFilter)
	if err != nil {
		t.Fatalf("count %s for %s: %v", tc.name, repositoryID, err)
	}
	if count.TotalFindings != want {
		t.Fatalf("count %s for %s = %d, want %d", tc.name, repositoryID, count.TotalFindings, want)
	}
	inventory, err := aggregateStore.SupplyChainImpactInventory(
		ctx,
		aggregateFilter,
		impact.SupplyChainImpactInventoryByImpactStatus,
		10,
		0,
	)
	if err != nil {
		t.Fatalf("inventory %s for %s: %v", tc.name, repositoryID, err)
	}
	if want == 0 && len(inventory) != 0 {
		t.Fatalf("inventory %s for %s = %#v, want empty", tc.name, repositoryID, inventory)
	}
	if want == 1 && (len(inventory) != 1 || inventory[0].Count != 1) {
		t.Fatalf("inventory %s for %s = %#v, want count %d", tc.name, repositoryID, inventory, want)
	}
	if tc.environment != "" {
		return
	}
	_, err = findingStore.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		CVEID:                runtimeFilterLiveCVE,
		PackageID:            packageID,
		WorkloadID:           tc.workloadID,
		ServiceID:            tc.serviceID,
		AllowedRepositoryIDs: []string{repositoryID},
	})
	if want == 0 && !errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
		t.Fatalf("explain %s for %s error = %v, want not found", tc.name, repositoryID, err)
	}
	if want == 1 && err != nil {
		t.Fatalf("explain %s for %s: %v", tc.name, repositoryID, err)
	}
}

func seedSupplyChainRuntimeRepositoryPrecedenceLiveFacts(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"raw-payload",
		runtimePrecedenceRawRepository,
		"pkg:deb/example/raw-payload",
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"related-scope",
		runtimePrecedenceRelatedRepository,
		"pkg:deb/example/related-scope",
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"raw-envelope",
		runtimePrecedenceEnvelopeRepository,
		"pkg:deb/example/raw-envelope",
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"decoder-decoy",
		runtimeFilterLiveDecoyRepo,
		runtimePrecedenceDecoyPackage,
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"whitespace-related",
		runtimePrecedenceWhitespaceRepo,
		"pkg:deb/example/whitespace-related",
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"scalar-related",
		runtimePrecedenceScalarRepo,
		"pkg:deb/example/scalar-related",
	)
	insertRuntimePrecedenceFinding(
		t,
		ctx,
		tx,
		"later-related",
		runtimePrecedenceLaterRepo,
		"pkg:deb/example/later-related",
	)

	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:workload:raw-payload",
		runtimeFilterLiveDecoyRepo,
		runtimeFilterLiveGenDecoy,
		impact.WorkloadIdentityFactKindQuery,
		false,
		map[string]any{
			"scope_id":    runtimePrecedenceRawRepository,
			"workload_id": "workload:5747:raw-payload",
		},
	)
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:service:related-before-fallback",
		runtimeFilterLiveDecoyRepo,
		runtimeFilterLiveGenDecoy,
		serviceCatalogCorrelationFactKind,
		false,
		map[string]any{
			"scope_id":          runtimePrecedenceRawRepository,
			"related_scope_ids": []string{runtimePrecedenceRelatedRepository},
			"service_id":        "service:5747:related-scope",
			"outcome":           "exact",
		},
	)
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:environment:raw-envelope",
		runtimePrecedenceEnvelopeRepository,
		runtimePrecedenceEnvelopeGeneration,
		cicdRunCorrelationFactKind,
		false,
		map[string]any{
			"environment": "raw-envelope-5747",
			"outcome":     "exact",
		},
	)
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:workload:whitespace-related",
		runtimeFilterLiveDecoyRepo,
		runtimeFilterLiveGenDecoy,
		impact.WorkloadIdentityFactKindQuery,
		false,
		map[string]any{
			"scope_id":          runtimePrecedenceRawRepository,
			"related_scope_ids": []string{"  " + runtimePrecedenceWhitespaceRepo + "  "},
			"workload_id":       "workload:5747:whitespace-related",
		},
	)
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:service:scalar-related",
		runtimeFilterLiveDecoyRepo,
		runtimeFilterLiveGenDecoy,
		serviceCatalogCorrelationFactKind,
		false,
		map[string]any{
			"scope_id":          runtimePrecedenceRawRepository,
			"related_scope_ids": "  " + runtimePrecedenceScalarRepo + "  ",
			"service_id":        "service:5747:scalar-related",
			"outcome":           "exact",
		},
	)
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5747:workload:later-related",
		runtimeFilterLiveDecoyRepo,
		runtimeFilterLiveGenDecoy,
		impact.WorkloadIdentityFactKindQuery,
		false,
		map[string]any{
			"scope_id": runtimePrecedenceRawRepository,
			"related_scope_ids": []string{
				"git-repository-scope:   ",
				"  " + runtimePrecedenceLaterRepo + "  ",
			},
			"workload_id": "workload:5747:later-related",
		},
	)
}

func insertRuntimePrecedenceFinding(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	suffix string,
	repositoryID string,
	packageID string,
) {
	t.Helper()
	factID := "fact:5747:precedence:" + suffix
	findingID := "finding:5747:precedence:" + suffix
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		factID,
		runtimeFilterLiveScopeA,
		runtimeFilterLiveGenA,
		impact.SupplyChainImpactFindingFactKind,
		false,
		map[string]any{
			"finding_id":        findingID,
			"cve_id":            runtimeFilterLiveCVE,
			"package_id":        packageID,
			"repository_id":     repositoryID,
			"impact_status":     "affected_exact",
			"detection_profile": "comprehensive",
			"priority_score":    "30",
			"priority_bucket":   "medium",
			"suppression_state": "active",
			"service_ids":       []string{},
			"workload_ids":      []string{},
			"environments":      []string{},
			"evidence_fact_ids": []string{},
		},
	)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id,
  priority_score, source_count, impact_status, ecosystem, severity_bucket,
  repository_id, cve_id, package_id, priority_bucket, detection_profile,
  suppression_state, materialized_at
) VALUES (
  $1, $2, $3, $4,
  30, 1, 'affected_exact', 'deb', 'medium',
  $5, $6, $7, 'medium', 'comprehensive',
  'active', NOW()
)`,
		"canonical:5747:precedence:"+suffix,
		factID,
		runtimeFilterLiveScopeA,
		findingID,
		repositoryID,
		runtimeFilterLiveCVE,
		packageID,
	); err != nil {
		t.Fatalf("insert precedence winner %s: %v", suffix, err)
	}
}
