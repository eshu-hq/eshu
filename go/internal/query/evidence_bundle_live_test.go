// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// evidenceBundleFixtureSnapshot is the shared RawSnapshot fixture for the
// live-bundle handler tests: one stage, one domain backlog with a matching
// queue blockage, one coordinator-registered collector, and a
// not-configured semantic provider.
func evidenceBundleFixtureSnapshot() statuspkg.RawSnapshot {
	fixed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: fixed,
		ScopeActivity: statuspkg.ScopeActivitySnapshot{
			Active: 5, Changed: 1, Unchanged: 4,
		},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active: 1, Completed: 9, Superseded: 5, Other: 2,
		},
		StageCounts: []statuspkg.StageStatusCount{
			{Stage: "parse", Status: "pending", Count: 2},
			{Stage: "parse", Status: "claimed", Count: 6},
			{Stage: "parse", Status: "running", Count: 3},
			{Stage: "parse", Status: "succeeded", Count: 11},
		},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{
				Domain:      "aws_relationship_materialization",
				Outstanding: 5,
				InFlight:    4,
				DeadLetter:  1,
				OldestAge:   41500 * time.Millisecond,
			},
		},
		QueueBlockages: []statuspkg.QueueBlockage{
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k1", Blocked: 3},
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k2", Blocked: 2},
		},
		Queue: statuspkg.QueueSnapshot{
			Total: 18, Outstanding: 7, Pending: 4, InFlight: 2, Retrying: 1,
			Succeeded: 10, DeadLetter: 1, OverdueClaims: 3,
			OldestOutstandingAge: 12500 * time.Millisecond,
		},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{
					InstanceID: "git-1", CollectorKind: "git", Mode: "direct",
					Enabled: true, ClaimsEnabled: true, DisplayName: "git",
					LastObservedAt: fixed, UpdatedAt: fixed,
				},
			},
		},
		SemanticExtraction: statuspkg.SemanticExtractionStatus{
			State: "unavailable", Reason: "provider_not_configured", ProviderConfigured: false,
		},
	}
}

// evidenceBundleFixtureGraph is a GraphQuery stub returning a fixed
// repository count for MATCH (r:Repository) RETURN count(r), matching what
// getIndexStatus's identical query would report against the same graph.
type evidenceBundleFixtureGraph struct {
	count int
	err   error
}

func (g evidenceBundleFixtureGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, errors.New("Run not implemented by evidenceBundleFixtureGraph")
}

func (g evidenceBundleFixtureGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if g.err != nil {
		return nil, g.err
	}
	return map[string]any{"count": g.count}, nil
}

func TestEvidenceHandlerLiveBundleMissingStatusReaderReturns503(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestEvidenceHandlerLiveBundleComposesValidatedBundle(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: evidenceBundleFixtureSnapshot()},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	if got, want := bundle.SchemaVersion, evidencebundle.SchemaVersion; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := bundle.Validation.Status, "passed"; got != want {
		t.Fatalf("Validation.Status = %q, want %q -- the route must stamp validation only after Validate returns nil", got, want)
	}
	// The acceptance test for #4045: the served bundle must itself pass the
	// exact evidencebundle.Validate the CLI runs, not merely echo a status
	// string the handler wrote.
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("evidencebundle.Validate(bundle) error = %v, want nil; body = %s", err, rec.Body.String())
	}

	state := bundle.Contents.PipelineState
	if state == nil {
		t.Fatal("Contents.PipelineState = nil, want populated from the stub status snapshot")
	}
	if state.RepositoryCount != 5 {
		t.Fatalf("RepositoryCount = %d, want 5", state.RepositoryCount)
	}
	if state.QueueBlockedCount != 5 {
		t.Fatalf("QueueBlockedCount = %d, want 5 (3+2 gated rows summed across the two blockages)", state.QueueBlockedCount)
	}
	if len(state.DomainBacklogs) != 1 || state.DomainBacklogs[0].Blocked != 3 {
		t.Fatalf("DomainBacklogs = %+v, want one aws_relationship_materialization row with blocked=3 (the max of the two blockage rows)", state.DomainBacklogs)
	}
	if len(state.Collectors) != 1 || state.Collectors[0].CollectorKind != "git" {
		t.Fatalf("Collectors = %+v, want the one coordinator-registered git collector from the stub", state.Collectors)
	}
	provider := bundle.Contents.SemanticProviderState
	if provider == nil || provider.State != "unavailable" || provider.Reason != "provider_not_configured" {
		t.Fatalf("SemanticProviderState = %+v, want unavailable/provider_not_configured from the stub", provider)
	}
}

func TestEvidenceHandlerLiveBundleFailedGraphReadReportsZeroRepositoryCount(t *testing.T) {
	t.Parallel()

	// Proves the bundle route degrades the same way getIndexStatus does on a
	// failed graph read (status.go getIndexStatus: qErr != nil leaves
	// repoCount at its zero value) rather than failing the whole bundle.
	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: evidenceBundleFixtureSnapshot()},
		Neo4j:        evidenceBundleFixtureGraph{err: errors.New("graph unavailable")},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	if bundle.Contents.PipelineState.RepositoryCount != 0 {
		t.Fatalf("RepositoryCount = %d, want 0 on a failed graph read", bundle.Contents.PipelineState.RepositoryCount)
	}
	foundRepoCountGap := false
	for _, missing := range bundle.Missing {
		if missing.Family == "repository_count" {
			foundRepoCountGap = true
		}
	}
	if !foundRepoCountGap {
		t.Fatal("Missing does not record the repository_count ambiguous-zero gap (evidencebundle.BuildLiveBundle)")
	}
}

// TestEvidenceHandlerLiveBundleFlagsTruncatedDomainBacklogs proves the route
// tells the caller when status.Report capped DomainBacklogs (#4045 review):
// composing more non-empty domains than status.DefaultOptions().DomainLimit
// must not silently drop rows -- the bundle's Bounds must say so.
func TestEvidenceHandlerLiveBundleFlagsTruncatedDomainBacklogs(t *testing.T) {
	t.Parallel()

	snapshot := evidenceBundleFixtureSnapshot()
	snapshot.DomainBacklogs = nil
	for i := 0; i < 7; i++ {
		snapshot.DomainBacklogs = append(snapshot.DomainBacklogs, statuspkg.DomainBacklog{
			Domain:      string(rune('a' + i)),
			Outstanding: 7 - i,
		})
	}
	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: snapshot},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	if len(bundle.Contents.PipelineState.DomainBacklogs) != 5 {
		t.Fatalf("DomainBacklogs = %+v, want 5 rows (status.DefaultOptions().DomainLimit cap)", bundle.Contents.PipelineState.DomainBacklogs)
	}
	if !bundle.Bounds.Truncated {
		t.Fatal("Bounds.Truncated = false, want true: 7 domains exceed the 5-row cap")
	}
	var flagged bool
	for _, layer := range bundle.Bounds.TruncatedLayers {
		if layer == "domain_backlogs" {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("Bounds.TruncatedLayers = %v, want it to include \"domain_backlogs\"", bundle.Bounds.TruncatedLayers)
	}
}

// TestAuthMiddlewareWithScopedTokensRejectsEvidenceBundleRoute proves the
// tenant-scoping decision for GET /api/v0/evidence/bundle against the REAL
// handler path -- the real EvidenceHandler mounted on a real mux, wrapped by
// the real AuthMiddlewareWithScopedTokens -- not an interface mock over the
// allowlist matcher. The bundle composes stack-wide status data (repository
// count, queue, every domain's backlog, every collector), the same data GET
// /api/v0/status/index and GET /api/v0/status/pipeline already refuse scoped
// tokens for, so a scoped bearer token must never reach the handler.
func TestAuthMiddlewareWithScopedTokensRejectsEvidenceBundleRoute(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: evidenceBundleFixtureSnapshot()},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
			AllScopes:   true,
		},
		ok: true,
	}
	wrapped := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; a scoped bearer token reached the stack-wide live evidence bundle handler; body = %s", got, want, rec.Body.String())
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsSharedTokenEvidenceBundleRoute is
// the positive control for the rejection test above: the same real handler
// and mux, reached through the shared-token path AuthMiddlewareWithScopedTokens
// falls back to when no bearer resolver claims the credential.
func TestAuthMiddlewareWithScopedTokensAllowsSharedTokenEvidenceBundleRoute(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: evidenceBundleFixtureSnapshot()},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	resolver := &fakeScopedTokenResolver{ok: false}
	wrapped := AuthMiddlewareWithScopedTokens("shared-secret", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
	req.Header.Set("Authorization", "Bearer shared-secret")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}
