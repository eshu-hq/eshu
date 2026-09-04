// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

// stubCloudInventory is a CloudResourceCurrentInventoryFilter stub: it returns
// only the candidate uids present in `currentAuthorized`, modelling the
// owner-ledger current-inventory + authorization gate. It records the candidate
// uids and whether the caller was unscoped.
type stubCloudInventory struct {
	currentAuthorized map[string]struct{}
	rowsByDigest      map[string][]map[string]any
	err               error
	gotDigests        []string
	gotCandidates     []string
	gotAllScopes      bool
}

func (s *stubCloudInventory) CurrentAuthorizedCloudResourceUIDs(
	_ context.Context, candidateUIDs []string, allScopes bool, _ []string, _ []string,
) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.gotCandidates = append([]string(nil), candidateUIDs...)
	s.gotAllScopes = allScopes
	var out []string
	for _, uid := range candidateUIDs {
		if _, ok := s.currentAuthorized[uid]; ok {
			out = append(out, uid)
		}
	}
	return out, nil
}

func (s *stubCloudInventory) CurrentAuthorizedCloudResourcesByDigest(
	_ context.Context, digests []string, allScopes bool, _ []string, _ []string,
) ([]CloudResourceRuntimeDigestMatch, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.gotDigests = append([]string(nil), digests...)
	s.gotAllScopes = allScopes
	var out []CloudResourceRuntimeDigestMatch
	for _, digest := range digests {
		for _, row := range s.rowsByDigest[digest] {
			uid := StringVal(row, "uid")
			s.gotCandidates = append(s.gotCandidates, uid)
			if _, ok := s.currentAuthorized[uid]; !ok {
				continue
			}
			out = append(out, CloudResourceRuntimeDigestMatch{
				UID:    uid,
				Digest: StringVal(row, "digest"),
				ARN:    StringVal(row, "arn"),
			})
		}
	}
	return out, nil
}

func cloudResourceGraphRow(uid, digest, arn string) map[string]any {
	return map[string]any{"uid": uid, "digest": digest, "arn": arn}
}

func TestProbeSupplyChainCloudRuntimeResourcesRecordsTruthfulCounts(t *testing.T) {
	tests := []struct {
		name       string
		inventory  *stubCloudInventory
		wantCounts map[string]int64
	}{
		{
			name: "zero authorized current resources",
			inventory: &stubCloudInventory{
				currentAuthorized: map[string]struct{}{},
				rowsByDigest:      map[string][]map[string]any{},
			},
			wantCounts: map[string]int64{
				"eshu.subject_digest_count":              1,
				"eshu.authorized_current_resource_count": 0,
				"eshu.runtime_confirmed_digest_count":    0,
				"eshu.runtime_resource_count":            0,
			},
		},
		{
			name: "authorized current resources",
			inventory: &stubCloudInventory{
				currentAuthorized: map[string]struct{}{
					"CloudResource:synthetic:runtime-a": {},
					"CloudResource:synthetic:runtime-b": {},
				},
				rowsByDigest: map[string][]map[string]any{
					"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {
						cloudResourceGraphRow(
							"CloudResource:synthetic:runtime-a",
							"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
							"arn:example:compute:::resource/runtime-a",
						),
						cloudResourceGraphRow(
							"CloudResource:synthetic:runtime-b",
							"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
							"arn:example:compute:::resource/runtime-b",
						),
					},
				},
			},
			wantCounts: map[string]int64{
				"eshu.subject_digest_count":              1,
				"eshu.authorized_current_resource_count": 2,
				"eshu.runtime_confirmed_digest_count":    1,
				"eshu.runtime_resource_count":            2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previousTracer := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("supply-chain-cloud-runtime-probe-test")
			t.Cleanup(func() { queryHandlerTracer = previousTracer })

			handler := &SupplyChainHandler{CloudResourceInventory: tt.inventory}
			_, err := handler.probeSupplyChainCloudRuntimeResources(
				context.Background(),
				repositoryAccessFilter{AllScopes: true},
				[]string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			)
			if err != nil {
				t.Fatalf("probeSupplyChainCloudRuntimeResources() error = %v, want nil", err)
			}

			spans := recorder.Ended()
			if got, want := len(spans), 1; got != want {
				t.Fatalf("ended spans = %d, want %d", got, want)
			}
			if got, want := spans[0].Name(), "supply_chain.cloud_runtime_probe"; got != want {
				t.Fatalf("span name = %q, want %q", got, want)
			}
			attributes := make(map[string]any)
			for _, item := range spans[0].Attributes() {
				attributes[string(item.Key)] = item.Value.AsInterface()
			}
			if _, exists := attributes["eshu.candidate_resource_count"]; exists {
				t.Fatalf("span attributes contain false pre-authorization candidate count: %#v", attributes)
			}
			for key, want := range tt.wantCounts {
				if got := attributes[key]; got != want {
					t.Fatalf("span attribute %s = %#v, want %#v; attributes=%#v", key, got, want, attributes)
				}
			}
		})
	}
}

func TestApplySupplyChainCloudRuntimeEvidencePromotesRunningDigest(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	uid := "CloudResource:aws:ecs:task-a"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/aaaaaaaa"
	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(uid, runningDigest, ecsARN)},
		},
	}
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest:      graph.rowsByDigest,
	}
	handler := &SupplyChainHandler{Neo4j: graph, CloudResourceInventory: inventory}

	rows := []impact.SupplyChainImpactFindingRow{
		{FindingID: "f-running", SubjectDigest: runningDigest, EvidencePath: []string{cicdRunCorrelationFactKind}},
		{FindingID: "f-notrunning", SubjectDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", EvidencePath: []string{cicdRunCorrelationFactKind}},
	}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence() error = %v, want nil", err)
	}

	if got := rows[0].CloudRuntimeResourceRefs; len(got) != 1 || got[0] != ecsARN {
		t.Fatalf("running finding CloudRuntimeResourceRefs = %#v, want [%q]", got, ecsARN)
	}
	if tier := impact.BuildSupplyChainImpactFindingResult(&rows[0]).DeploymentTruthTier; tier != "runtime_confirmed" {
		t.Fatalf("running finding tier = %q, want runtime_confirmed", tier)
	}
	if got := rows[1].CloudRuntimeResourceRefs; len(got) != 0 {
		t.Fatalf("non-running finding CloudRuntimeResourceRefs = %#v, want none", got)
	}
	if tier := impact.BuildSupplyChainImpactFindingResult(&rows[1]).DeploymentTruthTier; tier != "provenance_ci_declared" {
		t.Fatalf("non-running finding tier = %q, want provenance_ci_declared", tier)
	}
}

// TestApplySupplyChainCloudRuntimeEvidenceExcludesStaleOrUnauthorized is the
// #5452 P1a (staleness) + #5787 (authorization) proof: a CloudResource node that
// matches the digest in the graph but is NOT returned as current+authorized by
// the owner-ledger filter (a stale node, or one in a scope the caller is not
// granted) must NOT promote the finding — its ARN is never surfaced.
func TestApplySupplyChainCloudRuntimeEvidenceExcludesStaleOrUnauthorized(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	staleUID := "CloudResource:aws:ecs:task-gone"
	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(staleUID, runningDigest, "arn:aws:ecs:us-east-1:123456789012:task/demo/gone")},
		},
	}
	// The owner ledger admits nothing — the matched node is stale/unauthorized.
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{},
		rowsByDigest:      graph.rowsByDigest,
	}
	handler := &SupplyChainHandler{Neo4j: graph, CloudResourceInventory: inventory}

	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: runningDigest, EvidencePath: []string{cicdRunCorrelationFactKind}}}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence() error = %v, want nil", err)
	}
	if len(rows[0].CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("CloudRuntimeResourceRefs = %#v, want none for a stale/unauthorized node", rows[0].CloudRuntimeResourceRefs)
	}
	if tier := impact.BuildSupplyChainImpactFindingResult(&rows[0]).DeploymentTruthTier; tier != "provenance_ci_declared" {
		t.Fatalf("tier = %q, want provenance_ci_declared (no runtime evidence from a stale node)", tier)
	}
	if len(inventory.gotCandidates) != 1 || inventory.gotCandidates[0] != staleUID {
		t.Fatalf("owner-ledger filter candidates = %#v, want [%q]", inventory.gotCandidates, staleUID)
	}
}

// TestApplySupplyChainCloudRuntimeEvidenceScopedCallerGetsAuthorized proves a
// scoped-token caller now DOES receive runtime evidence for a cloud resource the
// owner ledger authorizes (the probe no longer blanket-skips scoped callers;
// #5787 resolved), and the ledger is asked with allScopes=false.
func TestApplySupplyChainCloudRuntimeEvidenceScopedCallerGetsAuthorized(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	uid := "CloudResource:aws:ecs:task-scoped"
	arn := "arn:aws:ecs:us-east-1:123456789012:task/demo/scoped"
	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{runningDigest: {cloudResourceGraphRow(uid, runningDigest, arn)}},
	}
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest:      graph.rowsByDigest,
	}
	handler := &SupplyChainHandler{Neo4j: graph, CloudResourceInventory: inventory}

	scoped := repositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:r_granted"}}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: runningDigest}}
	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), scoped, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence(scoped) error = %v, want nil", err)
	}
	if got := rows[0].CloudRuntimeResourceRefs; len(got) != 1 || got[0] != arn {
		t.Fatalf("scoped caller CloudRuntimeResourceRefs = %#v, want [%q] (authorized resource)", got, arn)
	}
	if inventory.gotAllScopes {
		t.Fatalf("owner-ledger filter called with allScopes=true for a scoped caller; want false")
	}
}

func TestApplySupplyChainCloudRuntimeEvidenceDoesNotReadGraph(t *testing.T) {
	t.Parallel()

	graph := &stubCloudRuntimeGraph{err: errors.New("graph unavailable")}
	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	uid := "CloudResource:synthetic:graph-free"
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest: map[string][]map[string]any{
			digest: {cloudResourceGraphRow(uid, digest, "arn:example:compute:::resource/graph-free")},
		},
	}
	handler := &SupplyChainHandler{Neo4j: graph, CloudResourceInventory: inventory}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: digest}}

	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainCloudRuntimeEvidence() error = %v, want graph-free owner-ledger read", err)
	}
	if len(graph.gotDigests) != 0 {
		t.Fatalf("graph digests = %#v, want no graph query", graph.gotDigests)
	}
}

func TestApplySupplyChainCloudRuntimeEvidencePropagatesLedgerError(t *testing.T) {
	t.Parallel()

	digest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	graph := &stubCloudRuntimeGraph{rowsByDigest: map[string][]map[string]any{digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")}}}
	inventory := &stubCloudInventory{
		err:          errors.New("ledger unavailable"),
		rowsByDigest: graph.rowsByDigest,
	}
	handler := &SupplyChainHandler{Neo4j: graph, CloudResourceInventory: inventory}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: digest}}

	if err := handler.applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err == nil {
		t.Fatal("applySupplyChainCloudRuntimeEvidence() error = nil, want the owner-ledger error propagated")
	}
}

func TestApplySupplyChainCloudRuntimeEvidenceNilStoresAreNoOp(t *testing.T) {
	t.Parallel()

	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: "sha256:cc"}}
	// Nil graph remains safe because runtime evidence resolves from Postgres.
	if err := (&SupplyChainHandler{CloudResourceInventory: &stubCloudInventory{}}).
		applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("nil graph error = %v, want nil", err)
	}
	// Nil inventory filter (disables the runtime tier rather than surfacing unauthorized evidence).
	if err := (&SupplyChainHandler{Neo4j: &stubCloudRuntimeGraph{}}).
		applySupplyChainCloudRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("nil inventory error = %v, want nil", err)
	}
	if len(rows[0].CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("CloudRuntimeResourceRefs = %#v, want none when a store is unwired", rows[0].CloudRuntimeResourceRefs)
	}
}
