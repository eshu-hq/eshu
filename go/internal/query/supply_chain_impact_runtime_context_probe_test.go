// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// runtimeContextFindingStore satisfies BOTH SupplyChainImpactFindingStore and
// the optional supplyChainImpactRuntimeContextReader capability, so probe
// tests exercise the handler's type-asserted read path without Postgres.
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

func osPackageFindingRowForRuntimeContext() impact.SupplyChainImpactFindingRow {
	return impact.SupplyChainImpactFindingRow{
		FindingID:     "finding-os-1",
		CVEID:         "CVE-2026-0001",
		PackageID:     "os://debian/openssl",
		Ecosystem:     "os",
		RepositoryID:  "repository:r_217415d9",
		SubjectDigest: "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab",
		ImpactStatus:  "affected_exact",
		MatchReason:   "dpkg_exact_affected_version",
		EvidencePath:  []string{facts.VulnerabilityOSPackageFactKind, facts.ScannerWorkerAnalysisFactKind},
	}
}

func TestApplySupplyChainRuntimeContextThreadsScopedGrants(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{byRepo: map[string]impact.SupplyChainRuntimeContext{}}
	handler := &SupplyChainHandler{ImpactFindings: store}
	access := repositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_217415d9"},
		AllowedScopeIDs:      []string{"scope:5747:tenant-a"},
	}

	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, access); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if got, want := store.allowedRepoIDs, access.AllowedRepositoryIDs; !equalPacketStringSlices(got, want) {
		t.Fatalf("allowed repository ids = %#v, want %#v", got, want)
	}
	if got, want := store.allowedScopeIDs, access.AllowedScopeIDs; !equalPacketStringSlices(got, want) {
		t.Fatalf("allowed scope ids = %#v, want %#v", got, want)
	}
}

func TestSupplyChainImpactRuntimeContextHandlerThreadsScopeOnlyGrant(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{
		rows: []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()},
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			"repository:r_217415d9": {ServiceIDs: []string{"service:5747:allowed"}},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&profile=comprehensive",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-5747-a",
		WorkspaceID:     "workspace-5747-a",
		AllowedScopeIDs: []string{"scope:5747:tenant-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := store.allowedScopeIDs, []string{"scope:5747:tenant-a"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("runtime-context allowed scope ids = %#v, want %#v", got, want)
	}
	if len(store.allowedRepoIDs) != 0 {
		t.Fatalf("runtime-context allowed repository ids = %#v, want empty", store.allowedRepoIDs)
	}
}

func TestApplySupplyChainRuntimeContextResolvesWorkloadsServicesEnvironments(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{byRepo: map[string]impact.SupplyChainRuntimeContext{
		"repository:r_217415d9": {
			WorkloadIDs:       []string{"workload:supply-chain-demo-db"},
			ServiceIDs:        []string{"service:demo-db"},
			DeploymentIDs:     []string{"deployment:demo-db-prod"},
			Environments:      []string{"production"},
			CatalogEntityRefs: []string{"component:demo-db"},
			CatalogOwnerRefs:  []string{"group:owners"},
		},
	}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	ctx := rows[0].RuntimeContext
	if ctx == nil {
		t.Fatal("RuntimeContext = nil, want populated read-time context")
	}
	if ctx.TruthBasis != "read_time_resolved" {
		t.Errorf("TruthBasis = %q, want read_time_resolved", ctx.TruthBasis)
	}
	if len(ctx.WorkloadIDs) != 1 || ctx.WorkloadIDs[0] != "workload:supply-chain-demo-db" {
		t.Errorf("WorkloadIDs = %v, want [workload:supply-chain-demo-db]", ctx.WorkloadIDs)
	}
	if len(ctx.ServiceIDs) != 1 || ctx.ServiceIDs[0] != "service:demo-db" {
		t.Errorf("ServiceIDs = %v, want [service:demo-db]", ctx.ServiceIDs)
	}
	if len(ctx.DeploymentIDs) != 1 || ctx.DeploymentIDs[0] != "deployment:demo-db-prod" {
		t.Errorf("DeploymentIDs = %v, want [deployment:demo-db-prod]", ctx.DeploymentIDs)
	}
	if len(ctx.Environments) != 1 || ctx.Environments[0] != "production" {
		t.Errorf("Environments = %v, want [production]", ctx.Environments)
	}
	if len(store.called) != 1 || store.called[0] != "repository:r_217415d9" {
		t.Errorf("reader repositories = %v, want [repository:r_217415d9]", store.called)
	}
	// The baked filter fields MUST NOT be backfilled from read-time context —
	// #5747 filters current runtime facts independently, so response enrichment
	// must leave the reducer-owned payload untouched.
	if len(rows[0].WorkloadIDs) != 0 {
		t.Errorf("baked WorkloadIDs = %v, want untouched empty (no backfill)", rows[0].WorkloadIDs)
	}
}

func TestApplySupplyChainRuntimeContextKeepsRepeatedDigestEvidenceWithinRowPlan(t *testing.T) {
	t.Parallel()

	first := osPackageFindingRowForRuntimeContext()
	first.RepositoryID = "repository:r_first"
	first.Environments = []string{"production"}
	second := first
	second.FindingID = "finding-os-2"
	second.RepositoryID = "repository:r_second"
	second.Environments = []string{"staging"}
	store := &runtimeContextFindingStore{
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			first.RepositoryID:  {},
			second.RepositoryID: {},
		},
		byDigest: map[string]map[string]string{
			first.SubjectDigest: {
				"production": impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent,
				"staging":    impact.SupplyChainRuntimeEnvironmentEvidenceDeclared,
			},
		},
	}
	rows := []impact.SupplyChainImpactFindingRow{first, second}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		repositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	for index, want := range []struct {
		environment string
		evidence    string
	}{
		{environment: "production", evidence: impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent},
		{environment: "staging", evidence: impact.SupplyChainRuntimeEnvironmentEvidenceDeclared},
	} {
		contextValue := rows[index].RuntimeContext
		if contextValue == nil {
			t.Fatalf("row %d runtime context = nil", index)
		}
		if len(contextValue.Environments) != 1 || contextValue.Environments[0] != want.environment {
			t.Fatalf("row %d environments = %#v, want [%s]", index, contextValue.Environments, want.environment)
		}
		if len(contextValue.EnvironmentEvidence) != 1 || contextValue.EnvironmentEvidence[want.environment] != want.evidence {
			t.Fatalf("row %d evidence = %#v, want %s=%s", index, contextValue.EnvironmentEvidence, want.environment, want.evidence)
		}
	}
}

func TestApplySupplyChainRuntimeContextCapsRepeatedDigestPageEvidenceAtCandidateBudget(t *testing.T) {
	t.Parallel()

	const rowCount = supplyChainImpactFindingMaxLimit
	const repositoryID = "repository:r_repeated_digest_budget"
	rows := make([]impact.SupplyChainImpactFindingRow, rowCount)
	confirmed := make(map[string]string, rowCount)
	for index := range rows {
		environment := fmt.Sprintf("environment-%03d", index)
		rows[index] = osPackageFindingRowForRuntimeContext()
		rows[index].FindingID = fmt.Sprintf("finding-os-%03d", index)
		rows[index].RepositoryID = repositoryID
		rows[index].Environments = []string{environment}
		confirmed[environment] = impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent
	}
	store := &runtimeContextFindingStore{
		byRepo: map[string]impact.SupplyChainRuntimeContext{repositoryID: {}},
		byDigest: map[string]map[string]string{
			rows[0].SubjectDigest: confirmed,
		},
	}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		repositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	totalEvidence := 0
	for index, row := range rows {
		wantEnvironment := fmt.Sprintf("environment-%03d", index)
		if row.RuntimeContext == nil {
			t.Fatalf("row %d runtime context = nil", index)
		}
		if got := row.RuntimeContext.EnvironmentEvidence; len(got) != 1 || got[wantEnvironment] != impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent {
			t.Fatalf("row %d evidence = %#v, want only %s=deploy_event", index, got, wantEnvironment)
		}
		probe := row.RuntimeContext.EnvironmentEvidenceProbe
		if probe == nil || probe.CandidateLimit != 1 || probe.CandidatesTruncated {
			t.Fatalf("row %d probe = %#v, want limit=1 truncated=false", index, probe)
		}
		totalEvidence += len(row.RuntimeContext.EnvironmentEvidence)
	}
	if totalEvidence > maxSupplyChainRuntimeEnvironmentCandidates {
		t.Fatalf("serialized exact-digest evidence entries = %d, want <= %d", totalEvidence, maxSupplyChainRuntimeEnvironmentCandidates)
	}
}

func TestApplySupplyChainRuntimeContextHonestEmptyForRepoWithNoWorkloads(t *testing.T) {
	t.Parallel()

	// Repo exists but has no workload/service/env facts yet (fresh ingest):
	// the context is present and labeled, with empty lists — not an error,
	// not a silently-missing field a caller could misread as "never scanned".
	store := &runtimeContextFindingStore{byRepo: map[string]impact.SupplyChainRuntimeContext{}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	ctx := rows[0].RuntimeContext
	if ctx == nil {
		t.Fatal("RuntimeContext = nil, want honest empty context for repo with no runtime facts")
	}
	if ctx.TruthBasis != "read_time_resolved" {
		t.Errorf("TruthBasis = %q, want read_time_resolved", ctx.TruthBasis)
	}
	if len(ctx.WorkloadIDs) != 0 || len(ctx.ServiceIDs) != 0 || len(ctx.Environments) != 0 {
		t.Errorf("context = %+v, want empty lists", ctx)
	}
}

func TestApplySupplyChainRuntimeContextSkipsFindingWithNoRepositoryAnchor(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{byRepo: map[string]impact.SupplyChainRuntimeContext{
		"repository:r_217415d9": {WorkloadIDs: []string{"workload:x"}},
	}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	row := osPackageFindingRowForRuntimeContext()
	row.RepositoryID = ""
	rows := []impact.SupplyChainImpactFindingRow{row}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if rows[0].RuntimeContext != nil {
		t.Errorf("RuntimeContext = %+v, want nil for finding with no repository anchor", rows[0].RuntimeContext)
	}
	if len(store.called) != 0 {
		t.Errorf("reader called with %v, want no call when nothing to resolve", store.called)
	}
}

func TestApplySupplyChainRuntimeContextPropagatesReaderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("postgres: connection reset")
	store := &runtimeContextFindingStore{err: wantErr}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true})
	if !errors.Is(err, wantErr) {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want %v", err, wantErr)
	}
}

func TestApplySupplyChainRuntimeContextDeterministicOrdering(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{byRepo: map[string]impact.SupplyChainRuntimeContext{
		"repository:r_217415d9": {
			WorkloadIDs:   []string{"workload:b", "workload:a"},
			ServiceIDs:    []string{"service:b", "service:a"},
			DeploymentIDs: []string{"deployment:b", "deployment:a"},
			Environments:  []string{"staging", "production"},
		},
	}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	ctx := rows[0].RuntimeContext
	if ctx.WorkloadIDs[0] != "workload:a" || ctx.WorkloadIDs[1] != "workload:b" {
		t.Errorf("WorkloadIDs = %v, want sorted [workload:a workload:b]", ctx.WorkloadIDs)
	}
	if ctx.Environments[0] != "production" || ctx.Environments[1] != "staging" {
		t.Errorf("Environments = %v, want sorted [production staging]", ctx.Environments)
	}
}

func TestApplySupplyChainRuntimeContextStoreWithoutReaderIsNoOp(t *testing.T) {
	t.Parallel()

	// A handler whose store does not implement the runtime-context reader
	// (legacy store or a test double) leaves rows untouched rather than
	// erroring — the feature degrades to the pre-#5746 response shape.
	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	rows := []impact.SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{AllScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if rows[0].RuntimeContext != nil {
		t.Errorf("RuntimeContext = %+v, want nil without a reader", rows[0].RuntimeContext)
	}
}
