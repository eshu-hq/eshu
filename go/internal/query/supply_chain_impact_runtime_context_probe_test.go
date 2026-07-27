// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// runtimeContextFindingStore satisfies BOTH SupplyChainImpactFindingStore and
// the optional supplyChainImpactRuntimeContextReader capability, so probe
// tests exercise the handler's type-asserted read path without Postgres.
type runtimeContextFindingStore struct {
	rows            []SupplyChainImpactFindingRow
	byRepo          map[string]SupplyChainRuntimeContext
	called          []string
	allowedRepoIDs  []string
	allowedScopeIDs []string
	err             error
}

func (f *runtimeContextFindingStore) ListSupplyChainImpactFindings(
	context.Context,
	SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	return append([]SupplyChainImpactFindingRow(nil), f.rows...), nil
}

func (f *runtimeContextFindingStore) ListSupplyChainImpactRuntimeContext(
	_ context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]SupplyChainRuntimeContext, error) {
	f.called = append([]string(nil), repositoryIDs...)
	f.allowedRepoIDs = append([]string(nil), allowedRepositoryIDs...)
	f.allowedScopeIDs = append([]string(nil), allowedScopeIDs...)
	if f.err != nil {
		return nil, f.err
	}
	return f.byRepo, nil
}

func osPackageFindingRowForRuntimeContext() SupplyChainImpactFindingRow {
	return SupplyChainImpactFindingRow{
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

	store := &runtimeContextFindingStore{byRepo: map[string]SupplyChainRuntimeContext{}}
	handler := &SupplyChainHandler{ImpactFindings: store}
	access := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repository:r_217415d9"},
		allowedScopeIDs:      []string{"scope:5747:tenant-a"},
	}

	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, access); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if got, want := store.allowedRepoIDs, access.allowedRepositoryIDs; !equalPacketStringSlices(got, want) {
		t.Fatalf("allowed repository ids = %#v, want %#v", got, want)
	}
	if got, want := store.allowedScopeIDs, access.allowedScopeIDs; !equalPacketStringSlices(got, want) {
		t.Fatalf("allowed scope ids = %#v, want %#v", got, want)
	}
}

func TestSupplyChainImpactRuntimeContextHandlerThreadsScopeOnlyGrant(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{
		rows: []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()},
		byRepo: map[string]SupplyChainRuntimeContext{
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

	store := &runtimeContextFindingStore{byRepo: map[string]SupplyChainRuntimeContext{
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

	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true}); err != nil {
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

func TestApplySupplyChainRuntimeContextHonestEmptyForRepoWithNoWorkloads(t *testing.T) {
	t.Parallel()

	// Repo exists but has no workload/service/env facts yet (fresh ingest):
	// the context is present and labeled, with empty lists — not an error,
	// not a silently-missing field a caller could misread as "never scanned".
	store := &runtimeContextFindingStore{byRepo: map[string]SupplyChainRuntimeContext{}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true}); err != nil {
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

	store := &runtimeContextFindingStore{byRepo: map[string]SupplyChainRuntimeContext{
		"repository:r_217415d9": {WorkloadIDs: []string{"workload:x"}},
	}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	row := osPackageFindingRowForRuntimeContext()
	row.RepositoryID = ""
	rows := []SupplyChainImpactFindingRow{row}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true}); err != nil {
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

	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true})
	if !errors.Is(err, wantErr) {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want %v", err, wantErr)
	}
}

func TestApplySupplyChainRuntimeContextDeterministicOrdering(t *testing.T) {
	t.Parallel()

	store := &runtimeContextFindingStore{byRepo: map[string]SupplyChainRuntimeContext{
		"repository:r_217415d9": {
			WorkloadIDs:   []string{"workload:b", "workload:a"},
			ServiceIDs:    []string{"service:b", "service:a"},
			DeploymentIDs: []string{"deployment:b", "deployment:a"},
			Environments:  []string{"staging", "production"},
		},
	}}
	handler := &SupplyChainHandler{ImpactFindings: store}

	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true}); err != nil {
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
	rows := []SupplyChainImpactFindingRow{osPackageFindingRowForRuntimeContext()}
	if err := handler.applySupplyChainRuntimeContext(context.Background(), rows, repositoryAccessFilter{allScopes: true}); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}
	if rows[0].RuntimeContext != nil {
		t.Errorf("RuntimeContext = %+v, want nil without a reader", rows[0].RuntimeContext)
	}
}
