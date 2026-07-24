// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// stubCloudRuntimeGraph is a GraphQuery stub for the #5452 runtime-image probe.
// It returns rowsByDigest for any digest present in the query params and records
// the digest list the probe passed, so a test can assert both the promotion
// outcome and that the probe bounded/deduplicated its input.
type stubCloudRuntimeGraph struct {
	rowsByDigest map[string][]map[string]any
	err          error
	gotDigests   []string
}

func (s *stubCloudRuntimeGraph) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	digests, _ := params["digests"].([]string)
	s.gotDigests = append([]string(nil), digests...)
	var rows []map[string]any
	for _, digest := range digests {
		rows = append(rows, s.rowsByDigest[digest]...)
	}
	return rows, nil
}

func (s *stubCloudRuntimeGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func cloudResourceGraphRow(digest, arn string) map[string]any {
	return map[string]any{"digest": digest, "arn": arn}
}

func TestApplySupplyChainCloudRuntimeEvidencePromotesRunningDigest(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/aaaaaaaa"
	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(runningDigest, ecsARN)},
		},
	}
	handler := &SupplyChainHandler{Neo4j: graph}

	rows := []SupplyChainImpactFindingRow{
		{FindingID: "f-running", SubjectDigest: runningDigest, EvidencePath: []string{cicdRunCorrelationFactKind}},
		{FindingID: "f-notrunning", SubjectDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", EvidencePath: []string{cicdRunCorrelationFactKind}},
	}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{allScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence() error = %v, want nil", err)
	}

	// The running finding gains the resource ref and classifies runtime_confirmed.
	if got := rows[0].CloudRuntimeResourceRefs; len(got) != 1 || got[0] != ecsARN {
		t.Fatalf("running finding CloudRuntimeResourceRefs = %#v, want [%q]", got, ecsARN)
	}
	if tier := buildSupplyChainImpactFindingResult(rows[0]).DeploymentTruthTier; tier != "runtime_confirmed" {
		t.Fatalf("running finding tier = %q, want runtime_confirmed", tier)
	}
	// The non-running finding keeps its CI-declared tier, no fabricated refs.
	if got := rows[1].CloudRuntimeResourceRefs; len(got) != 0 {
		t.Fatalf("non-running finding CloudRuntimeResourceRefs = %#v, want none", got)
	}
	if tier := buildSupplyChainImpactFindingResult(rows[1]).DeploymentTruthTier; tier != "provenance_ci_declared" {
		t.Fatalf("non-running finding tier = %q, want provenance_ci_declared", tier)
	}
	// Both distinct digests were probed (deduped, non-blank).
	if len(graph.gotDigests) != 2 {
		t.Fatalf("probed digests = %#v, want the 2 distinct finding digests", graph.gotDigests)
	}
}

func TestApplySupplyChainCloudRuntimeEvidencePropagatesProbeError(t *testing.T) {
	t.Parallel()

	graph := &stubCloudRuntimeGraph{err: errors.New("graph unavailable")}
	handler := &SupplyChainHandler{Neo4j: graph}
	rows := []SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: "sha256:cc"}}

	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{allScopes: true}, rows); err == nil {
		t.Fatal("applySupplyChainCloudRuntimeEvidence() error = nil, want the probe error propagated (never a silent false config_only)")
	}
}

// TestApplySupplyChainCloudRuntimeEvidenceSkipsScopedCaller is the #5452 F1
// scope-authorization proof: a scoped-token caller must NOT receive
// runtime-observed cloud evidence, because CloudResource graph nodes carry no
// scope_id and the probe cannot authorize matched resources through the owner
// ledger — surfacing them would leak ARNs of cloud resources in scopes the
// caller is not granted. The probe is skipped entirely (no graph query issued)
// and the finding keeps its non-runtime tier.
func TestApplySupplyChainCloudRuntimeEvidenceSkipsScopedCaller(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(runningDigest, "arn:aws:ecs:us-east-1:123456789012:task/demo/aaaaaaaa")},
		},
	}
	handler := &SupplyChainHandler{Neo4j: graph}
	rows := []SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: runningDigest, EvidencePath: []string{cicdRunCorrelationFactKind}}}

	scoped := repositoryAccessFilter{allowedRepositoryIDs: []string{"repository:r_only"}}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), scoped, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence(scoped) error = %v, want nil", err)
	}
	if len(rows[0].CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("scoped caller CloudRuntimeResourceRefs = %#v, want none (no cross-scope ARN leak)", rows[0].CloudRuntimeResourceRefs)
	}
	if len(graph.gotDigests) != 0 {
		t.Fatalf("scoped caller issued a graph probe (digests %#v); it must be skipped entirely", graph.gotDigests)
	}
	if tier := buildSupplyChainImpactFindingResult(rows[0]).DeploymentTruthTier; tier != "provenance_ci_declared" {
		t.Fatalf("scoped caller tier = %q, want provenance_ci_declared (CI-declared, not runtime)", tier)
	}
}

func TestApplySupplyChainCloudRuntimeEvidenceNilGraphIsNoOp(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	rows := []SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: "sha256:cc"}}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{allScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence() with nil graph error = %v, want nil", err)
	}
	if len(rows[0].CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("CloudRuntimeResourceRefs = %#v, want none when no graph is wired", rows[0].CloudRuntimeResourceRefs)
	}
}
