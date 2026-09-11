// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func TestAddSupplyChainRuntimeContextFactCICDRejectedEvidenceDoesNotFold(t *testing.T) {
	t.Parallel()

	for _, payload := range []map[string]any{
		{
			"repository_id":        "repository:r_rejected",
			"environment":          "production",
			"environment_evidence": "deploy_event",
			"outcome":              "rejected",
		},
		{
			"repository_id":        "repository:r_rejected",
			"environment":          "production",
			"environment_evidence": "deploy_event",
			"outcome":              "exact",
			"provenance_only":      true,
		},
	} {
		out := map[string]impact.SupplyChainRuntimeContext{}
		impact.AddSupplyChainRuntimeContextFact(out, cloudRuntimeProbeTestCICDFactKind, "scope", payload)
		if _, ok := out["repository:r_rejected"]; ok {
			t.Fatalf("rejected payload folded into runtime context: %#v", payload)
		}
	}
}

func TestApplySupplyChainRuntimeContextUsesRepositoryEnvironmentOnlyAsExactDigestCandidate(t *testing.T) {
	t.Parallel()

	contextValue := impact.SupplyChainRuntimeContext{
		Environments: []string{"production"},
	}
	row := osPackageFindingRowForRuntimeContext()
	store := &querytestutil.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.SupplyChainRuntimeContext{
			"repository:r_217415d9": contextValue,
		},
		ByDigest: map[string]map[string]string{
			row.SubjectDigest: {"production": "deploy_event"},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	rows := []impact.SupplyChainImpactFindingRow{row}

	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, querycontract.RepositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	got := rows[0].RuntimeContext.EnvironmentEvidence
	if got["production"] != "deploy_event" {
		t.Fatalf("runtime_context.environment_evidence = %#v, want production=deploy_event", got)
	}
	if rows[0].EnvironmentEvidence != nil {
		t.Fatalf("baked EnvironmentEvidence = %#v, want untouched nil", rows[0].EnvironmentEvidence)
	}
	if len(rows[0].Environments) != 0 {
		t.Fatalf("baked Environments = %#v, want untouched empty", rows[0].Environments)
	}
}

func TestApplySupplyChainRuntimeContextDoesNotDefaultUnconfirmedRepositoryEnvironment(t *testing.T) {
	t.Parallel()

	store := &querytestutil.FakeRuntimeContextFindingStore{ByRepo: map[string]impact.SupplyChainRuntimeContext{
		"repository:r_217415d9": {Environments: []string{"production"}},
	}}
	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if got := rows[0].RuntimeContext.EnvironmentEvidence; len(got) != 0 {
		t.Fatalf("runtime environment evidence = %#v, want empty without exact digest match", got)
	}
}

func TestApplySupplyChainRuntimeContextCarriesCurrentDigestBoundEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	store := &querytestutil.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.SupplyChainRuntimeContext{},
		ByDigest: map[string]map[string]string{
			row.SubjectDigest: {"production": "deploy_event"},
		},
	}
	rows := []impact.SupplyChainImpactFindingRow{row}
	rows[0].Environments = []string{"production"}

	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if got := rows[0].RuntimeContext.EnvironmentEvidence["production"]; got != "deploy_event" {
		t.Fatalf("runtime environment evidence = %q, want current digest-bound deploy_event", got)
	}
	if got := rows[0].RuntimeContext.EnvironmentEvidenceProbe; got == nil || got.CandidateLimit != 1 || got.CandidatesTruncated {
		t.Fatalf("environment evidence probe = %#v, want limit=1 truncated=false", got)
	}
}

func TestPlanSupplyChainRuntimeEnvironmentCandidatesSharesPageBudgetFairly(t *testing.T) {
	t.Parallel()

	rows := make([]impact.SupplyChainImpactFindingRow, SupplyChainImpactFindingMaxLimit)
	for i := range rows {
		rows[i] = impact.SupplyChainImpactFindingRow{
			FindingID:     "finding-" + strconv.Itoa(i),
			SubjectDigest: "sha256:" + strconv.Itoa(i),
			Environments:  []string{"staging", "production", "production"},
		}
	}
	candidates, plans := PlanSupplyChainRuntimeEnvironmentCandidates(rows, nil)
	if got, want := len(candidates), SupplyChainImpactFindingMaxLimit; got != want {
		t.Fatalf("SQL candidates = %d, want %d", got, want)
	}
	for rowIndex, plan := range plans {
		if got := len(plan.candidates); got != 1 {
			t.Fatalf("row %d candidates = %d, want 1", rowIndex, got)
		}
		if plan.metadata == nil || plan.metadata.CandidateLimit != 1 || !plan.metadata.CandidatesTruncated {
			t.Fatalf("row %d metadata = %#v, want limit=1 truncated=true", rowIndex, plan.metadata)
		}
		if got := plan.candidates[0].Environment; got != "production" {
			t.Fatalf("row %d first sorted environment = %q, want production", rowIndex, got)
		}
	}
}

func TestPlanSupplyChainRuntimeEnvironmentCandidatesDeduplicatesSQLPairs(t *testing.T) {
	t.Parallel()

	rows := []impact.SupplyChainImpactFindingRow{
		{SubjectDigest: "sha256:shared", Environments: []string{"production"}},
		{SubjectDigest: "sha256:shared", Environments: []string{"production"}},
	}
	candidates, plans := PlanSupplyChainRuntimeEnvironmentCandidates(rows, nil)
	if got := len(candidates); got != 1 {
		t.Fatalf("SQL candidates = %d, want one deduplicated pair", got)
	}
	for rowIndex, plan := range plans {
		if plan.metadata == nil || plan.metadata.CandidateLimit != 1 || plan.metadata.CandidatesTruncated {
			t.Fatalf("row %d metadata = %#v, want limit=1 truncated=false", rowIndex, plan.metadata)
		}
	}
}

func TestApplySupplyChainRuntimeContextDefensivelyCopiesEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	sourceEvidence := map[string]string{"production": "deploy_event"}
	row := osPackageFindingRowForRuntimeContext()
	store := &querytestutil.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.SupplyChainRuntimeContext{
			row.RepositoryID: {Environments: []string{"production"}},
		},
		ByDigest: map[string]map[string]string{row.SubjectDigest: sourceEvidence},
	}
	rows := []impact.SupplyChainImpactFindingRow{row}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	rows[0].RuntimeContext.EnvironmentEvidence["production"] = "declared"
	if sourceEvidence["production"] != "deploy_event" {
		t.Fatalf("response mutation changed source map to %#v", sourceEvidence)
	}
	sourceEvidence["staging"] = "deploy_event"
	if _, exists := rows[0].RuntimeContext.EnvironmentEvidence["staging"]; exists {
		t.Fatalf("source mutation changed response map to %#v", rows[0].RuntimeContext.EnvironmentEvidence)
	}
}

func TestApplySupplyChainRuntimeContextOmitsOrphanEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	store := &querytestutil.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.SupplyChainRuntimeContext{
			row.RepositoryID: {Environments: []string{"production"}},
		},
		ByDigest: map[string]map[string]string{
			row.SubjectDigest: {
				"production": "deploy_event",
				"staging":    "deploy_event",
			},
		},
	}
	rows := []impact.SupplyChainImpactFindingRow{row}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	got := rows[0].RuntimeContext.EnvironmentEvidence
	if !reflect.DeepEqual(got, map[string]string{"production": "deploy_event"}) {
		t.Fatalf("environment evidence = %#v, want only resolved production evidence", got)
	}
}

func TestSupplyChainListAndExplainReportSameRuntimeEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	const repositoryID = "repository:r_environment_evidence_parity"
	finding := impact.SupplyChainImpactFindingRow{
		FindingID:     "finding-environment-evidence-parity",
		CVEID:         "CVE-2026-5835",
		PackageID:     "pkg:npm/example",
		ImpactStatus:  "affected_exact",
		RepositoryID:  repositoryID,
		SubjectDigest: "sha256:environment-evidence-parity",
		Environments:  []string{"production"},
	}
	contextValue := impact.SupplyChainRuntimeContext{
		Environments: []string{"production"},
	}
	contextStore := &querytestutil.FakeRuntimeContextFindingStore{
		Rows:   []impact.SupplyChainImpactFindingRow{finding},
		ByRepo: map[string]impact.SupplyChainRuntimeContext{repositoryID: contextValue},
		ByDigest: map[string]map[string]string{
			finding.SubjectDigest: {"production": "deploy_event"},
		},
	}
	handler := &SupplyChainHandler{
		ImpactFindings: contextStore,
		ImpactExplanations: &evidenceExplanationStore{
			row: impact.SupplyChainImpactExplanationRow{Finding: finding},
		},
		Readiness: &evidenceReadinessStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-5835&limit=10", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", list.Code, http.StatusOK, list.Body.String())
	}
	explain := httptest.NewRecorder()
	mux.ServeHTTP(explain, httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/explain?finding_id=finding-environment-evidence-parity", nil))
	if explain.Code != http.StatusOK {
		t.Fatalf("explain status = %d, want %d; body = %s", explain.Code, http.StatusOK, explain.Body.String())
	}

	listEvidence := nestedRuntimeEnvironmentEvidenceForTest(t, list.Body.Bytes(), "findings", "0", "runtime_context", "environment_evidence")
	explainEvidence := nestedRuntimeEnvironmentEvidenceForTest(t, explain.Body.Bytes(), "finding", "runtime_context", "environment_evidence")
	if !reflect.DeepEqual(listEvidence, explainEvidence) {
		t.Fatalf("list environment evidence = %#v, explain = %#v", listEvidence, explainEvidence)
	}
	if listEvidence["production"] != "deploy_event" {
		t.Fatalf("runtime environment evidence = %#v, want production=deploy_event", listEvidence)
	}
}

func BenchmarkFoldSupplyChainRuntimeContext200Repositories(b *testing.B) {
	const repositoryCount = 200
	type fact struct {
		kind    string
		scopeID string
		payload map[string]any
	}
	facts := make([]fact, 0, repositoryCount*4)
	for i := 0; i < repositoryCount; i++ {
		repositoryID := "repository:r_benchmark_" + strconv.Itoa(i)
		facts = append(
			facts,
			fact{kind: impact.WorkloadIdentityFactKindQuery, scopeID: repositoryID, payload: map[string]any{"workload_id": "workload:benchmark"}},
			fact{kind: runtimeFilterLiveServiceCatalogFactKind, scopeID: repositoryID, payload: map[string]any{"service_id": "service:benchmark", "outcome": "exact"}},
			fact{kind: impact.PlatformMaterializationFactKindQuery, scopeID: repositoryID, payload: map[string]any{"deployment_id": "deployment:benchmark"}},
			fact{kind: cloudRuntimeProbeTestCICDFactKind, scopeID: repositoryID, payload: map[string]any{"environment": "production", "environment_evidence": "deploy_event", "outcome": "exact"}},
		)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := make(map[string]impact.SupplyChainRuntimeContext, repositoryCount)
		for _, item := range facts {
			impact.AddSupplyChainRuntimeContextFact(out, item.kind, item.scopeID, item.payload)
		}
		if len(out) != repositoryCount {
			b.Fatalf("folded repositories = %d, want %d", len(out), repositoryCount)
		}
	}
}

func nestedRuntimeEnvironmentEvidenceForTest(t *testing.T, body []byte, path ...string) map[string]any {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v; body = %s", err, body)
	}
	for _, segment := range path {
		switch current := value.(type) {
		case map[string]any:
			value = current[segment]
		case []any:
			if segment != "0" || len(current) == 0 {
				t.Fatalf("path %v missing array entry %q in body %s", path, segment, body)
			}
			value = current[0]
		default:
			t.Fatalf("path %v stopped at %q (%T) in body %s", path, segment, value, body)
		}
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("path %v = %#v, want object", path, value)
	}
	return result
}

// evidenceExplanationStore and evidenceReadinessStore are minimal twins of
// root's evidenceExplanationStore and
// evidenceReadinessStore: the evidence parity tests only
// need canned rows/snapshots, not the filter-recording assertions the
// staying route tests pin. Keep the canned behavior identical; the root
// copies are authoritative for their suites.
type evidenceExplanationStore struct {
	row impact.SupplyChainImpactExplanationRow
	err error
}

func (s *evidenceExplanationStore) ExplainSupplyChainImpact(
	_ context.Context,
	_ impact.SupplyChainImpactExplanationFilter,
) (impact.SupplyChainImpactExplanationRow, error) {
	if s.err != nil {
		return impact.SupplyChainImpactExplanationRow{}, s.err
	}
	return s.row, nil
}

type evidenceReadinessStore struct {
	snapshot impact.SupplyChainImpactReadinessSnapshot
	err      error
}

func (s *evidenceReadinessStore) ReadSupplyChainImpactReadiness(
	_ context.Context,
	_ impact.SupplyChainImpactReadinessQuery,
) (impact.SupplyChainImpactReadinessSnapshot, error) {
	if s.err != nil {
		return impact.SupplyChainImpactReadinessSnapshot{}, s.err
	}
	return s.snapshot, nil
}
