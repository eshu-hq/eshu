// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type stubECSBindingCensus struct {
	result ECSBindingCensusResult
	err    error
	calls  int
}

func (s *stubECSBindingCensus) ListECSBindingDeclarations(context.Context) (ECSBindingCensusResult, error) {
	s.calls++
	return s.result, s.err
}

type stubECSBindingInstances struct {
	envs map[string][]string
}

func (s *stubECSBindingInstances) ListECSBindingInstanceEnvironments(_ context.Context, _ []string) (map[string][]string, error) {
	return s.envs, nil
}

func ecsBindingHandleCensus() ECSBindingCensusResult {
	return ECSBindingCensusResult{Declarations: []ECSBindingDeclaration{
		ecsBindingDeclaration("repo-app", "main.tf", "api", `"api"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
	}}
}

func ecsBindingHandleHandler(census *stubECSBindingCensus) (WorkloadCloudRelationshipMaterializationHandler, *recordingWorkloadCloudRelationshipWriter) {
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	writer := &recordingWorkloadCloudRelationshipWriter{}
	return WorkloadCloudRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			ecsBindingServiceEnvelope("fact-svc-api", "api", "prod", map[string]any{"environment": "prod"}),
			ecsBindingTaskEnvelope("fact-task-api", "api", "prod", []string{digest}),
		}},
		EdgeWriter:              writer,
		ReadinessLookup:         readyLookup(true, true),
		PriorGenerationCheck:    func(context.Context, string, string) (bool, error) { return true, nil },
		ECSDeclarationCensus:    census,
		ECSInstanceEnvironments: &stubECSBindingInstances{envs: map[string][]string{"api": {"prod"}}},
	}, writer
}

func TestECSBindingHandleWritesDeclarationRow(t *testing.T) {
	t.Parallel()

	census := &stubECSBindingCensus{result: ecsBindingHandleCensus()}
	handler, writer := ecsBindingHandleHandler(census)
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if census.calls != 1 {
		t.Fatalf("census calls = %d, want 1", census.calls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written rows = %d, want 1", len(writer.writtenRows))
	}
	row := writer.writtenRows[0]
	if got := anyToString(row["relationship_basis"]); got != ecsBindingBasisDeclaration {
		t.Errorf("basis = %q, want %q", got, ecsBindingBasisDeclaration)
	}
	if got := anyToString(row["resolution_mode"]); got != ecsBindingResolutionMode {
		t.Errorf("resolution_mode = %q, want %q", got, ecsBindingResolutionMode)
	}
	if got := anyToString(row["workload_id"]); got != "api" {
		t.Errorf("workload_id = %q, want bare workload name", got)
	}
	if !strings.Contains(result.EvidenceSummary, "1 ECS declaration-bound edge(s)") {
		t.Errorf("summary lacks ECS-bound count: %q", result.EvidenceSummary)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestECSBindingHandleNotesTruncatedCensus(t *testing.T) {
	t.Parallel()

	censusResult := ecsBindingHandleCensus()
	censusResult.Truncated = true
	census := &stubECSBindingCensus{result: censusResult}
	handler, _ := ecsBindingHandleHandler(census)
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(result.EvidenceSummary, "(declaration census truncated)") {
		t.Errorf("summary lacks truncation note: %q", result.EvidenceSummary)
	}
}

func TestECSBindingHandleUnwiredSkipsCensus(t *testing.T) {
	t.Parallel()

	handler, writer := workloadCloudGateHandler(&stubAffectedGraph{})
	handler.FactLoader = &stubFactLoader{envelopes: []facts.Envelope{
		ecsBindingServiceEnvelope("fact-svc-api", "api", "prod", map[string]any{"environment": "prod"}),
	}}
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.writtenRows) != 0 {
		t.Fatalf("unwired handler wrote %d rows, want 0", len(writer.writtenRows))
	}
	if strings.Contains(result.EvidenceSummary, "ECS declaration-bound") {
		t.Errorf("unwired summary mentions ECS binding: %q", result.EvidenceSummary)
	}
}

func TestECSBindingDeduplicatePrefersAnchorRow(t *testing.T) {
	t.Parallel()

	// Post-ladder triples: the Handle resolves both row sets through the
	// shared ladder first, so this dedup compares resolved workload ids
	// (pre-ladder spellings like "workload:api" vs "api" already agree).
	tally := newWorkloadCloudRelationshipTally()
	sibling := []map[string]any{{
		"workload_id": "workload-api-id", "environment": "prod",
		"cloud_resource_uid": "uid-1", "resolution_mode": "explicit_workload_anchor",
	}}
	ecs := []map[string]any{{
		"workload_id": "workload-api-id", "environment": "prod",
		"cloud_resource_uid": "uid-1", "resolution_mode": ecsBindingResolutionMode,
	}, {
		"workload_id": "workload-other-id", "environment": "prod",
		"cloud_resource_uid": "uid-1", "resolution_mode": ecsBindingResolutionMode,
	}}
	merged := ecsBindingDeduplicateRows(sibling, ecs, &tally)
	if len(merged) != 2 {
		t.Fatalf("merged rows = %d, want 2 (anchor + distinct-workload declaration)", len(merged))
	}
	if merged[0]["resolution_mode"] != "explicit_workload_anchor" {
		t.Errorf("anchor row lost its basis: %#v", merged[0])
	}
	if got := tally.skipped[ecsBindingSkipUsesEdgeExists]; got != 1 {
		t.Errorf("uses_edge_exists skips = %d, want 1", got)
	}
}

// stubMultiWorkloadSiblingLookup resolves workload names to canned ids.
type stubMultiWorkloadSiblingLookup struct {
	siblings []WorkloadSibling
}

func (s stubMultiWorkloadSiblingLookup) ListWorkloadSiblings(
	_ context.Context,
	_ []string,
) ([]WorkloadSibling, error) {
	return s.siblings, nil
}

func TestECSBindingHandleAnchorWinsTripleCollision(t *testing.T) {
	t.Parallel()

	// Two envelopes observe the same service: one carries an explicit
	// workload anchor (sibling path), one is bare (declaration path).
	// Both spellings ladder to one id, so the declaration row drops as
	// ecs_uses_edge_exists and the anchor basis survives.
	const digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	census := &stubECSBindingCensus{result: ecsBindingHandleCensus()}
	writer := &recordingWorkloadCloudRelationshipWriter{}
	handler := WorkloadCloudRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			ecsBindingServiceEnvelope("fact-svc-a", "api", "prod", map[string]any{
				"environment": "prod", "workload_id": "workload:api",
			}),
			ecsBindingServiceEnvelope("fact-svc-b", "api", "prod", map[string]any{"environment": "prod"}),
			ecsBindingTaskEnvelope("fact-task-api", "api", "prod", []string{digest}),
		}},
		EdgeWriter:              writer,
		ReadinessLookup:         readyLookup(true, true),
		PriorGenerationCheck:    func(context.Context, string, string) (bool, error) { return true, nil },
		ECSDeclarationCensus:    census,
		ECSInstanceEnvironments: &stubECSBindingInstances{envs: map[string][]string{"api": {"prod"}}},
		WorkloadLookup: stubMultiWorkloadSiblingLookup{siblings: []WorkloadSibling{
			{ID: "workload-api-id", Name: "api", RepoID: "repo-app"},
		}},
	}
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written rows = %d, want 1 (anchor wins)", len(writer.writtenRows))
	}
	if got := anyToString(writer.writtenRows[0]["relationship_basis"]); got != "aws_resource_service_anchor" {
		t.Errorf("surviving basis = %q, want the anchor basis", got)
	}
	if !strings.Contains(result.EvidenceSummary, "0 ECS declaration-bound edge(s)") {
		t.Errorf("summary lacks zero ECS-bound count: %q", result.EvidenceSummary)
	}
}

func TestECSBindingRowCarriesNoGrantIdentity(t *testing.T) {
	t.Parallel()

	// Scoped-caller safety parity: binding rows carry the same key set as
	// anchor rows (workload + service identity already visible to the
	// writer), plus declaration provenance. No repository grants, tokens,
	// or caller identity material may appear: the scoped query path never
	// reads USES edges (IdentityWithheld), and these rows must not smuggle
	// grant-scoped facts into the canonical edge.
	rows, _, _, err := ExtractWorkloadECSBindingRows(
		[]facts.Envelope{
			ecsBindingServiceEnvelope("fact-svc-api", "api", "prod", map[string]any{"environment": "prod"}),
			ecsBindingTaskEnvelope("fact-task-api", "api", "prod", []string{"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}),
		},
		ecsBindingHandleCensus(),
		map[string][]string{"api": {"prod"}},
	)
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("bound rows = %d, want 1", len(rows))
	}
	allowed := map[string]struct{}{
		"workload_id": {}, "cloud_resource_uid": {}, "relationship_type": {},
		"resolution_mode": {}, "environment": {}, "relationship_basis": {},
		"service_anchor_source": {}, "service_anchor_reason": {},
		"source_fact_id": {}, "stable_fact_key": {}, "source_system": {},
		"source_record_id": {}, "collector_kind": {},
		"declaration_repo_id": {}, "declaration_path": {},
	}
	for key := range rows[0] {
		if _, ok := allowed[key]; !ok {
			t.Errorf("binding row carries unexpected key %q (grant/identity leak surface)", key)
		}
	}
}
