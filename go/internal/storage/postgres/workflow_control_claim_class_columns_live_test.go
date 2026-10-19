// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8469 step 2 normalisation proof on real PostgreSQL: three-segment fairness keys with target class in claim_priority and the ecosystem in claim_ecosystem round-trip through enqueue and claim, the registry status surface counts rows by the real column, and migration 204 backfill classifies a legacy four-segment row; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowClaimClassColumnsRoundTrip proves #8469 step 2: the
// normalised three-segment key, the priority column, and the ecosystem
// column survive enqueue and claim, and the registry status surface reads
// the ecosystem column instead of the retired positional parse.
func TestWorkflowClaimClassColumnsRoundTrip(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	mustCreateRun(t, store, ctx, workflow.Run{
		RunID:       "claim-class-columns",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   base,
		UpdatedAt:   base,
	})
	newItem := func(id, key string, priority int, ecosystem string, offset time.Duration) workflow.WorkItem {
		created := base.Add(offset)
		return workflow.WorkItem{
			WorkItemID:          id,
			RunID:               "claim-class-columns",
			CollectorKind:       scope.CollectorPackageRegistry,
			CollectorInstanceID: "registry-instance",
			SourceSystem:        string(scope.CollectorPackageRegistry),
			ScopeID:             "scope-" + id,
			AcceptanceUnitID:    "scope-" + id,
			SourceRunID:         id + "-generation",
			GenerationID:        id + "-generation",
			FairnessKey:         key,
			ClaimPriority:       priority,
			ClaimEcosystem:      ecosystem,
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           created,
			UpdatedAt:           created,
		}
	}
	items := []workflow.WorkItem{
		newItem("priority-direct", "package_registry:registry-instance:npm", 0, "npm", 0),
		newItem("priority-broad", "package_registry:registry-instance:pypi", 3, "pypi", time.Second),
	}
	if err := store.EnqueueWorkItems(ctx, items); err != nil {
		t.Fatalf("EnqueueWorkItems error = %v, want nil", err)
	}

	// The claim still serves FIFO in step 2, so the first grant is the
	// oldest row; what this pins is that the class columns come back.
	granted, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "registry-instance",
		OwnerID:             "claim-class-columns-owner",
		ClaimID:             "claim-class-columns-0",
	}, base.Add(time.Hour).UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible error = %v, want nil", err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible found = false, want a grant")
	}
	if granted.WorkItemID != "priority-direct" {
		t.Fatalf("first grant = %q, want priority-direct (oldest)", granted.WorkItemID)
	}
	if granted.FairnessKey != "package_registry:registry-instance:npm" {
		t.Fatalf("FairnessKey = %q, want three segments", granted.FairnessKey)
	}
	if granted.ClaimPriority != 0 || granted.ClaimEcosystem != "npm" {
		t.Fatalf("ClaimPriority/ClaimEcosystem = %d/%q, want 0/npm",
			granted.ClaimPriority, granted.ClaimEcosystem)
	}

	counts, err := readRegistryMetadataTargetCounts(ctx, SQLDB{DB: database}, base.Add(time.Hour).UTC())
	if err != nil {
		t.Fatalf("readRegistryMetadataTargetCounts error = %v, want nil", err)
	}
	byEcosystem := make(map[string]int)
	for _, row := range counts[string(scope.CollectorPackageRegistry)] {
		byEcosystem[row.Ecosystem] = row.Planned
	}
	// priority-direct is claimed, so only its planned count remains visible
	// alongside the pending broad row; both ecosystems must be present.
	if byEcosystem["npm"] != 1 || byEcosystem["pypi"] != 1 {
		t.Fatalf("ecosystem planned counts = %v, want map[npm:1 pypi:1]", byEcosystem)
	}
}

// TestWorkflowClaimClassColumnsBackfill proves migration 204 keeps
// pre-normalisation backlog classed: a legacy four-segment row inserted at
// column defaults is invisible to the ecosystem status surface until the
// migration's own backfill UPDATEs run, and classified after. The UPDATEs
// are read from the migration file, never hand-copied, so the proof tracks
// the shipped backfill instead of a frozen copy.
func TestWorkflowClaimClassColumnsBackfill(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	mustCreateRun(t, store, ctx, workflow.Run{
		RunID:       "claim-class-backfill",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   base,
		UpdatedAt:   base,
	})
	legacy := workflow.WorkItem{
		WorkItemID:          "legacy-broad-row",
		RunID:               "claim-class-backfill",
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "registry-instance",
		SourceSystem:        string(scope.CollectorPackageRegistry),
		ScopeID:             "scope-legacy",
		AcceptanceUnitID:    "scope-legacy",
		SourceRunID:         "legacy-generation",
		GenerationID:        "legacy-generation",
		FairnessKey:         "package_registry:registry-instance:broad:rubygems",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           base,
		UpdatedAt:           base,
	}
	if err := store.EnqueueWorkItems(ctx, []workflow.WorkItem{legacy}); err != nil {
		t.Fatalf("EnqueueWorkItems error = %v, want nil", err)
	}

	countsBefore, err := readRegistryMetadataTargetCounts(ctx, SQLDB{DB: database}, base.Add(time.Hour).UTC())
	if err != nil {
		t.Fatalf("readRegistryMetadataTargetCounts error = %v, want nil", err)
	}
	for _, row := range countsBefore[string(scope.CollectorPackageRegistry)] {
		if row.Ecosystem == "rubygems" {
			t.Fatalf("unbackfilled legacy row is already counted: %#v", row)
		}
	}

	raw, err := os.ReadFile("migrations/204_workflow_work_items_claim_class_backfill.sql")
	if err != nil {
		t.Fatalf("read migration 204: %v", err)
	}
	applied := 0
	for _, stmt := range strings.Split(string(raw), ";") {
		trimmed := strings.TrimSpace(stmt)
		if !strings.HasPrefix(trimmed, "UPDATE") {
			continue
		}
		if _, err := database.ExecContext(ctx, trimmed); err != nil {
			t.Fatalf("apply migration 204 backfill: %v", err)
		}
		applied++
	}
	if applied != 2 {
		t.Fatalf("applied %d backfill UPDATEs from migration 204, want 2", applied)
	}

	granted, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "registry-instance",
		OwnerID:             "claim-class-backfill-owner",
		ClaimID:             "claim-class-backfill-0",
	}, base.Add(time.Hour).UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible error = %v, want nil", err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible found = false, want the backfilled row")
	}
	if granted.ClaimPriority != 3 || granted.ClaimEcosystem != "rubygems" {
		t.Fatalf("backfilled ClaimPriority/ClaimEcosystem = %d/%q, want 3/rubygems",
			granted.ClaimPriority, granted.ClaimEcosystem)
	}
}
