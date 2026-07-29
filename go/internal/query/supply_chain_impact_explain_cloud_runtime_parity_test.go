// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestSupplyChainListAndExplainReportSameDeploymentTruthForRuntimeConfirmedFinding
// is the codex P1-A regression proof: buildSupplyChainImpactFindingResult is a
// shared assembler reached by both GET .../impact/findings and
// GET .../impact/explain, but only the list handler applied the authorized
// cloud-runtime probe (applySupplyChainCloudRuntimeEvidence) before calling it.
// For a finding whose subject digest IS currently running on an authorized
// cloud resource, the two surfaces must agree: both report
// deployment_truth_tier=runtime_confirmed, version_resolution_tier=runtime_confirmed,
// and the identical version_resolution_corroboration set -- not the list
// endpoint reporting runtime_confirmed while explain silently falls back to
// provenance_ci_declared/config_only and omits the runtime corroboration.
func TestSupplyChainListAndExplainReportSameDeploymentTruthForRuntimeConfirmedFinding(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	uid := "CloudResource:aws:ecs:task-parity"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/dddddddd"

	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(uid, runningDigest, ecsARN)},
		},
	}
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest:      graph.rowsByDigest,
	}

	finding := SupplyChainImpactFindingRow{
		FindingID:                "finding-parity",
		CVEID:                    "CVE-2026-9001",
		PackageID:                "pkg:npm/example",
		ImpactStatus:             "affected_exact",
		SubjectDigest:            runningDigest,
		CIDeclaredArtifactDigest: runningDigest,
		EvidencePath:             []string{cicdRunCorrelationFactKind},
	}

	findingsStore := &recordingSupplyChainImpactFindingStore{rows: []SupplyChainImpactFindingRow{finding}}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: SupplyChainImpactExplanationRow{Finding: finding},
	}
	readiness := &recordingSupplyChainImpactReadinessStore{}

	handler := &SupplyChainHandler{
		ImpactFindings:         findingsStore,
		ImpactExplanations:     explanationStore,
		Readiness:              readiness,
		Neo4j:                  graph,
		CloudResourceInventory: inventory,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9001&limit=10", nil)
	listW := httptest.NewRecorder()
	mux.ServeHTTP(listW, listReq)
	if got, want := listW.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listW.Body.String())
	}
	var listResp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("list json.Unmarshal: %v", err)
	}
	if len(listResp.Findings) != 1 {
		t.Fatalf("list findings = %#v, want exactly 1", listResp.Findings)
	}
	listFinding := listResp.Findings[0]

	explainReq := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/explain?finding_id=finding-parity", nil)
	explainW := httptest.NewRecorder()
	mux.ServeHTTP(explainW, explainReq)
	if got, want := explainW.Code, http.StatusOK; got != want {
		t.Fatalf("explain status = %d, want %d; body = %s", got, want, explainW.Body.String())
	}
	var explainResp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(explainW.Body.Bytes(), &explainResp); err != nil {
		t.Fatalf("explain json.Unmarshal: %v", err)
	}
	if explainResp.Finding == nil {
		t.Fatalf("explain Finding = nil, want the explained finding")
	}
	explainFinding := *explainResp.Finding

	if listFinding.DeploymentTruthTier != "runtime_confirmed" {
		t.Fatalf("list deployment_truth_tier = %q, want runtime_confirmed", listFinding.DeploymentTruthTier)
	}
	if got, want := explainFinding.DeploymentTruthTier, listFinding.DeploymentTruthTier; got != want {
		t.Fatalf("explain deployment_truth_tier = %q, want %q (same as list) -- list and explain must agree on which tier won for the same finding", got, want)
	}
	if got, want := explainFinding.VersionResolutionTier, listFinding.VersionResolutionTier; got != want {
		t.Fatalf("explain version_resolution_tier = %q, want %q (same as list)", got, want)
	}
	if got, want := explainFinding.CloudRuntimeResourceRefs, listFinding.CloudRuntimeResourceRefs; !reflect.DeepEqual(got, want) {
		t.Fatalf("explain cloud_runtime_resource_refs = %#v, want %#v (same as list)", got, want)
	}
	if got, want := explainFinding.VersionResolutionCorroboration, listFinding.VersionResolutionCorroboration; !reflect.DeepEqual(got, want) {
		t.Fatalf("explain version_resolution_corroboration = %#v, want %#v (same as list)", got, want)
	}
}

// TestSupplyChainListAndExplainReportSameRuntimeContextForFindingThatHasOne
// is the third instance of the same shape as codex P1-A: buildSupplyChainImpactFindingResult's
// SupplyChainImpactFindingResult(row) conversion carries row.RuntimeContext
// straight through to the runtime_context response field
// (supply_chain_impact_result.go), but only the list route called
// applySupplyChainRuntimeContext before assembling results -- explain
// unconditionally reported no runtime_context even for a finding whose
// repository DOES currently resolve to real workload/service context.
func TestSupplyChainListAndExplainReportSameRuntimeContextForFindingThatHasOne(t *testing.T) {
	t.Parallel()

	repositoryID := "repository:r_context_parity"
	finding := SupplyChainImpactFindingRow{
		FindingID:    "finding-context-parity",
		CVEID:        "CVE-2026-9002",
		PackageID:    "pkg:npm/example",
		ImpactStatus: "affected_exact",
		RepositoryID: repositoryID,
	}

	contextStore := &runtimeContextFindingStore{
		rows: []SupplyChainImpactFindingRow{finding},
		byRepo: map[string]SupplyChainRuntimeContext{
			repositoryID: {
				WorkloadIDs: []string{"workload:example-api"},
				ServiceIDs:  []string{"service:example-api"},
			},
		},
	}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: SupplyChainImpactExplanationRow{Finding: finding},
	}
	readiness := &recordingSupplyChainImpactReadinessStore{}

	handler := &SupplyChainHandler{
		ImpactFindings:     contextStore,
		ImpactExplanations: explanationStore,
		Readiness:          readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9002&limit=10", nil)
	listW := httptest.NewRecorder()
	mux.ServeHTTP(listW, listReq)
	if got, want := listW.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listW.Body.String())
	}
	var listResp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("list json.Unmarshal: %v", err)
	}
	if len(listResp.Findings) != 1 {
		t.Fatalf("list findings = %#v, want exactly 1", listResp.Findings)
	}
	listFinding := listResp.Findings[0]
	if listFinding.RuntimeContext == nil {
		t.Fatalf("list runtime_context = nil, want a resolved context (test setup bug, not the fix under test)")
	}

	explainReq := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/explain?finding_id=finding-context-parity", nil)
	explainW := httptest.NewRecorder()
	mux.ServeHTTP(explainW, explainReq)
	if got, want := explainW.Code, http.StatusOK; got != want {
		t.Fatalf("explain status = %d, want %d; body = %s", got, want, explainW.Body.String())
	}
	var explainResp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(explainW.Body.Bytes(), &explainResp); err != nil {
		t.Fatalf("explain json.Unmarshal: %v", err)
	}
	if explainResp.Finding == nil {
		t.Fatalf("explain Finding = nil, want the explained finding")
	}

	if got, want := explainResp.Finding.RuntimeContext, listFinding.RuntimeContext; !reflect.DeepEqual(got, want) {
		t.Fatalf("explain runtime_context = %#v, want %#v (same as list) -- list and explain must agree on a finding's current workload/service context", got, want)
	}
}

// TestSupplyChainPacketSkipsEnrichmentThatItsWireShapeCannotExpose prevents the
// packet route from paying for runtime enrichment whose fields are absent from
// the transformed packet response.
func TestSupplyChainPacketSkipsEnrichmentThatItsWireShapeCannotExpose(t *testing.T) {
	t.Parallel()

	runningDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	uid := "CloudResource:aws:ecs:task-packet-parity"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/eeeeeeee"
	repositoryID := "repository:r_packet_parity"

	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(uid, runningDigest, ecsARN)},
		},
	}
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest:      graph.rowsByDigest,
	}

	finding := SupplyChainImpactFindingRow{
		FindingID:     "finding-packet-parity",
		CVEID:         "CVE-2026-9003",
		PackageID:     "pkg:npm/example",
		ImpactStatus:  "affected_exact",
		SubjectDigest: runningDigest,
		RepositoryID:  repositoryID,
	}
	contextStore := &runtimeContextFindingStore{
		byRepo: map[string]SupplyChainRuntimeContext{
			repositoryID: {WorkloadIDs: []string{"workload:example-api"}},
		},
	}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: SupplyChainImpactExplanationRow{Finding: finding},
	}
	readiness := &recordingSupplyChainImpactReadinessStore{}

	handler := &SupplyChainHandler{
		ImpactFindings:         contextStore,
		ImpactExplanations:     explanationStore,
		Readiness:              readiness,
		Neo4j:                  graph,
		CloudResourceInventory: inventory,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-packet-parity", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("packet status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	if len(graph.gotDigests) != 0 {
		t.Fatalf("cloud-runtime probe digests = %#v, want none for a packet shape that omits runtime evidence", graph.gotDigests)
	}
	if len(inventory.gotCandidates) != 0 {
		t.Fatalf("cloud-runtime probe candidates = %#v, want none", inventory.gotCandidates)
	}
	if len(contextStore.called) != 0 {
		t.Fatalf("runtime-context probe repository ids = %#v, want none for a packet shape that omits runtime_context", contextStore.called)
	}
}
