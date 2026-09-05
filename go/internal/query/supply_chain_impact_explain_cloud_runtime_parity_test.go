// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/truth"
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
	ecsARN := "arn:example:compute:::resource/dddddddd"

	graph := &stubCloudRuntimeGraph{
		rowsByDigest: map[string][]map[string]any{
			runningDigest: {cloudResourceGraphRow(uid, runningDigest, ecsARN)},
		},
	}
	inventory := &stubCloudInventory{
		currentAuthorized: map[string]struct{}{uid: {}},
		rowsByDigest:      graph.rowsByDigest,
	}

	finding := impact.SupplyChainImpactFindingRow{
		FindingID:                "finding-parity",
		CVEID:                    "CVE-2026-9001",
		PackageID:                "pkg:npm/example",
		ImpactStatus:             "affected_exact",
		SubjectDigest:            runningDigest,
		CIDeclaredArtifactDigest: runningDigest,
		EvidencePath:             []string{cicdRunCorrelationFactKind},
	}

	findingsStore := &recordingSupplyChainImpactFindingStore{rows: []impact.SupplyChainImpactFindingRow{finding}}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: impact.SupplyChainImpactExplanationRow{Finding: finding},
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
		Findings []impact.SupplyChainImpactFindingResult `json:"findings"`
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
	var explainResp impact.SupplyChainImpactExplanationResult
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

func TestSupplyChainListAndExplainReportSameKubernetesRuntimeEvidence(t *testing.T) {
	t.Parallel()

	digest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	graph := &stubKubernetesRuntimeGraph{rows: []map[string]any{{
		"matched_digest": digest, "workload_uid": "kw-parity", "edge_scope_id": "edge-scope", "edge_generation_id": "edge-generation",
	}}}
	inventory := &stubKubernetesWorkloadInventory{rows: []KubernetesRuntimeWorkloadMatch{{
		Digest: digest,
		WorkloadRef: impact.KubernetesRuntimeWorkloadRef{
			UID: "kw-parity", ClusterID: "cluster-a", Namespace: "payments", Name: "api",
		},
	}}}
	finding := impact.SupplyChainImpactFindingRow{
		FindingID: "finding-kubernetes-parity", CVEID: "CVE-2026-5834",
		PackageID: "pkg:npm/example", ImpactStatus: "affected_exact", SubjectDigest: digest,
	}
	handler := &SupplyChainHandler{
		ImpactFindings:              &recordingSupplyChainImpactFindingStore{rows: []impact.SupplyChainImpactFindingRow{finding}},
		ImpactExplanations:          &recordingSupplyChainImpactExplanationStore{row: impact.SupplyChainImpactExplanationRow{Finding: finding}},
		Readiness:                   &recordingSupplyChainImpactReadinessStore{},
		Neo4j:                       graph,
		KubernetesWorkloadInventory: inventory,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	listW := httptest.NewRecorder()
	mux.ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-5834&limit=10", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listW.Code, http.StatusOK, listW.Body.String())
	}
	var listResp struct {
		Findings []impact.SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil || len(listResp.Findings) != 1 {
		t.Fatalf("list response decode: err=%v findings=%#v", err, listResp.Findings)
	}

	explainW := httptest.NewRecorder()
	mux.ServeHTTP(explainW, httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/explain?finding_id=finding-kubernetes-parity", nil))
	if explainW.Code != http.StatusOK {
		t.Fatalf("explain status = %d, want %d; body=%s", explainW.Code, http.StatusOK, explainW.Body.String())
	}
	var explainResp impact.SupplyChainImpactExplanationResult
	if err := json.Unmarshal(explainW.Body.Bytes(), &explainResp); err != nil || explainResp.Finding == nil {
		t.Fatalf("explain response decode: err=%v finding=%#v", err, explainResp.Finding)
	}

	listFinding := listResp.Findings[0]
	explainFinding := *explainResp.Finding
	if listFinding.DeploymentTruthTier != string(truth.TierRuntimeConfirmed) {
		t.Fatalf("list deployment truth tier = %q, want %q", listFinding.DeploymentTruthTier, truth.TierRuntimeConfirmed)
	}
	if explainFinding.DeploymentTruthTier != listFinding.DeploymentTruthTier {
		t.Fatalf("explain deployment truth tier = %q, want list tier %q", explainFinding.DeploymentTruthTier, listFinding.DeploymentTruthTier)
	}
	if !reflect.DeepEqual(explainFinding.KubernetesRuntimeWorkloadRefs, listFinding.KubernetesRuntimeWorkloadRefs) {
		t.Fatalf("explain workload refs = %#v, want list refs %#v", explainFinding.KubernetesRuntimeWorkloadRefs, listFinding.KubernetesRuntimeWorkloadRefs)
	}
	if !reflect.DeepEqual(explainFinding.KubernetesRuntimeProbe, listFinding.KubernetesRuntimeProbe) {
		t.Fatalf("explain probe metadata = %#v, want list metadata %#v", explainFinding.KubernetesRuntimeProbe, listFinding.KubernetesRuntimeProbe)
	}
	if got, _ := graph.snapshot(); got != 2 {
		t.Fatalf("graph Run calls = %d, want one per route", got)
	}
}

func TestSupplyChainListAndExplainMapKubernetesGraphUnavailable(t *testing.T) {
	t.Parallel()

	digest := "sha256:abababababababababababababababababababababababababababababababab"
	finding := impact.SupplyChainImpactFindingRow{
		FindingID: "finding-kubernetes-error", CVEID: "CVE-2026-5835",
		PackageID: "pkg:npm/example", ImpactStatus: "affected_exact", SubjectDigest: digest,
	}
	handler := &SupplyChainHandler{
		ImpactFindings:              &recordingSupplyChainImpactFindingStore{rows: []impact.SupplyChainImpactFindingRow{finding}},
		ImpactExplanations:          &recordingSupplyChainImpactExplanationStore{row: impact.SupplyChainImpactExplanationRow{Finding: finding}},
		Readiness:                   &recordingSupplyChainImpactReadinessStore{},
		Neo4j:                       &stubKubernetesRuntimeGraph{err: ErrGraphUnavailable},
		KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-5835&limit=10",
		"/api/v0/supply-chain/impact/explain?finding_id=finding-kubernetes-error",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", EnvelopeMIMEType)
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s status = %d, want %d; body=%s", target, response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"code":"`+string(ErrorCodeBackendUnavailable)+`"`) {
			t.Fatalf("GET %s body=%s, want code %q", target, response.Body.String(), ErrorCodeBackendUnavailable)
		}
	}
}

func TestSupplyChainListAndExplainMapDriverlessKubernetesGraphUnavailable(t *testing.T) {
	t.Parallel()

	digest := "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	finding := impact.SupplyChainImpactFindingRow{
		FindingID: "finding-kubernetes-driverless", CVEID: "CVE-2026-5834",
		PackageID: "pkg:npm/example", ImpactStatus: "affected_exact", SubjectDigest: digest,
	}
	handler := &SupplyChainHandler{
		ImpactFindings:              &recordingSupplyChainImpactFindingStore{rows: []impact.SupplyChainImpactFindingRow{finding}},
		ImpactExplanations:          &recordingSupplyChainImpactExplanationStore{row: impact.SupplyChainImpactExplanationRow{Finding: finding}},
		Readiness:                   &recordingSupplyChainImpactReadinessStore{},
		Neo4j:                       NewNeo4jReader(nil, "nornic"),
		KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-5834&limit=10",
		"/api/v0/supply-chain/impact/explain?finding_id=finding-kubernetes-driverless",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", EnvelopeMIMEType)
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s status = %d, want %d; body=%s", target, response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"code":"`+string(ErrorCodeBackendUnavailable)+`"`) {
			t.Fatalf("GET %s body=%s, want code %q", target, response.Body.String(), ErrorCodeBackendUnavailable)
		}
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
	finding := impact.SupplyChainImpactFindingRow{
		FindingID:    "finding-context-parity",
		CVEID:        "CVE-2026-9002",
		PackageID:    "pkg:npm/example",
		ImpactStatus: "affected_exact",
		RepositoryID: repositoryID,
	}

	contextStore := &runtimeContextFindingStore{
		rows: []impact.SupplyChainImpactFindingRow{finding},
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			repositoryID: {
				WorkloadIDs: []string{"workload:example-api"},
				ServiceIDs:  []string{"service:example-api"},
			},
		},
	}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: impact.SupplyChainImpactExplanationRow{Finding: finding},
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
		Findings []impact.SupplyChainImpactFindingResult `json:"findings"`
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
	var explainResp impact.SupplyChainImpactExplanationResult
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
	ecsARN := "arn:example:compute:::resource/eeeeeeee"
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

	finding := impact.SupplyChainImpactFindingRow{
		FindingID:     "finding-packet-parity",
		CVEID:         "CVE-2026-9003",
		PackageID:     "pkg:npm/example",
		ImpactStatus:  "affected_exact",
		SubjectDigest: runningDigest,
		RepositoryID:  repositoryID,
	}
	contextStore := &runtimeContextFindingStore{
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			repositoryID: {WorkloadIDs: []string{"workload:example-api"}},
		},
	}
	explanationStore := &recordingSupplyChainImpactExplanationStore{
		row: impact.SupplyChainImpactExplanationRow{Finding: finding},
	}
	readiness := &recordingSupplyChainImpactReadinessStore{}

	handler := &SupplyChainHandler{
		ImpactFindings:         contextStore,
		ImpactExplanations:     explanationStore,
		Readiness:              readiness,
		Neo4j:                  graph,
		CloudResourceInventory: inventory,
		PacketResponder:        NewSupplyChainImpactPacketResponder(),
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

// Cloud-runtime probe doubles for the parity tests above. Canonical copies
// live with the moved cloud probe suite in internal/query/supplychain
// (supply_chain_impact_cloud_runtime_probe_test.go); this twin exists
// because the parity suite also drives the explain handler through the same
// fake inventory and cannot import the hub test package. Keep both copies
// behavior-identical; the hub copy is authoritative.

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

// Kubernetes-runtime probe doubles for the parity tests above. Canonical
// copies live with the moved probe suite in internal/query/supplychain
// (supply_chain_impact_kubernetes_runtime_probe_test.go); this twin exists
// because the parity suite also drives the findings/explain handlers through
// the same fake graph and inventory and cannot import the hub test package.
// The Run filtering logic is identical; only the recorded-call element type
// differs (the hub records fairKubernetesRuntimeCall, unexported there, so
// this twin records its own shape — the parity tests only assert the call
// count). Keep the Run logic behavior-identical; the hub copy is
// authoritative.

// parityKubernetesRuntimeCall records one per-digest probe query the twin
// graph served.
type parityKubernetesRuntimeCall struct {
	Digest string
	Limit  int
}

type stubKubernetesRuntimeGraph struct {
	mu       sync.Mutex
	rows     []map[string]any
	err      error
	cypher   string
	params   map[string]any
	runCalls int
	calls    []parityKubernetesRuntimeCall
}

func (s *stubKubernetesRuntimeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runCalls++
	s.cypher = cypher
	s.params = params
	digests, _ := params["subject_digests"].([]string)
	if len(digests) == 1 {
		s.calls = append(s.calls, parityKubernetesRuntimeCall{Digest: digests[0], Limit: IntVal(params, "limit")})
	}
	if s.err != nil || len(digests) != 1 {
		return nil, s.err
	}
	filtered := make([]map[string]any, 0, len(s.rows))
	for _, row := range s.rows {
		if StringVal(row, "matched_digest") == digests[0] {
			filtered = append(filtered, row)
		}
	}
	if limit := IntVal(params, "limit"); len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *stubKubernetesRuntimeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (s *stubKubernetesRuntimeGraph) snapshot() (int, []parityKubernetesRuntimeCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCalls, append([]parityKubernetesRuntimeCall(nil), s.calls...)
}

type stubKubernetesWorkloadInventory struct {
	rows         []KubernetesRuntimeWorkloadMatch
	err          error
	candidates   []KubernetesRuntimeCandidate
	allScopes    bool
	repositories []string
	scopes       []string
}

func (s *stubKubernetesWorkloadInventory) CurrentAuthorizedKubernetesRuntimeWorkloads(
	_ context.Context,
	candidates []KubernetesRuntimeCandidate,
	allScopes bool,
	repositories []string,
	scopes []string,
) ([]KubernetesRuntimeWorkloadMatch, error) {
	s.candidates = append([]KubernetesRuntimeCandidate(nil), candidates...)
	s.allScopes = allScopes
	s.repositories = append([]string(nil), repositories...)
	s.scopes = append([]string(nil), scopes...)
	return append([]KubernetesRuntimeWorkloadMatch(nil), s.rows...), s.err
}

// runtimeContextFindingStore satisfies BOTH SupplyChainImpactFindingStore and
// the optional supplyChainImpactRuntimeContextReader capability, so the
// parity tests exercise the handler's type-asserted read path without
// Postgres. Canonical copy lives with the moved runtime-context suite in
// internal/query/supplychain
// (supply_chain_impact_runtime_context_probe_test.go); this twin exists
// because the parity suite also drives the findings/explain handlers through
// the same fake and cannot import the hub test package. Keep both copies
// behavior-identical; the hub copy is authoritative.
type runtimeContextFindingStore struct {
	rows            []impact.SupplyChainImpactFindingRow
	byRepo          map[string]impact.SupplyChainRuntimeContext
	byDigest        map[string]map[string]string
	called          []string
	envCandidates   []impact.SupplyChainRuntimeEnvironmentCandidate
	allowedRepoIDs  []string
	allowedScopeIDs []string
	err             error
}

func (f *runtimeContextFindingStore) ListSupplyChainImpactRuntimeEnvironmentEvidence(
	_ context.Context,
	candidates []impact.SupplyChainRuntimeEnvironmentCandidate,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]map[string]string, error) {
	f.envCandidates = append([]impact.SupplyChainRuntimeEnvironmentCandidate(nil), candidates...)
	f.allowedRepoIDs = append([]string(nil), allowedRepositoryIDs...)
	f.allowedScopeIDs = append([]string(nil), allowedScopeIDs...)
	if f.err != nil {
		return nil, f.err
	}
	return f.byDigest, nil
}

func (f *runtimeContextFindingStore) ListSupplyChainImpactFindings(
	context.Context,
	impact.SupplyChainImpactFindingFilter,
) ([]impact.SupplyChainImpactFindingRow, error) {
	return append([]impact.SupplyChainImpactFindingRow(nil), f.rows...), nil
}

func (f *runtimeContextFindingStore) ListSupplyChainImpactRuntimeContext(
	_ context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]impact.SupplyChainRuntimeContext, error) {
	f.called = append([]string(nil), repositoryIDs...)
	f.allowedRepoIDs = append([]string(nil), allowedRepositoryIDs...)
	f.allowedScopeIDs = append([]string(nil), allowedScopeIDs...)
	if f.err != nil {
		return nil, f.err
	}
	return f.byRepo, nil
}
