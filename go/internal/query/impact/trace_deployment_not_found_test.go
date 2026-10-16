// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// nilImpactTraceContext answers the deployment-trace service context with no
// workload and no error: the release-name 404 path (#7767 Gap 3).
type nilImpactTraceContext struct{}

func (nilImpactTraceContext) FetchServiceTraceContext(
	context.Context, querycontract.GraphQuery, querycontract.ContentStore,
	*slog.Logger, *telemetry.Instruments, string, TraceEnrichmentConfig,
) (map[string]any, error) {
	return nil, nil
}

func (nilImpactTraceContext) BuildServiceDeploymentOverview(map[string]any) map[string]any {
	return nil
}

// exactNameContentStub answers SearchEntitiesByExactNameAnyRepo from a fixed
// fixture with the production predicate (exact name and type), and counts the
// probes so tests can prove an empty grant probes nothing.
type exactNameContentStub struct {
	content.FakePortContentStore
	entities []querycontract.EntityContent
	probes   *atomic.Int64
}

func (s exactNameContentStub) SearchEntitiesByExactNameAnyRepo(
	_ context.Context, entityType, name string, _ int,
) ([]querycontract.EntityContent, error) {
	if s.probes != nil {
		s.probes.Add(1)
	}
	var out []querycontract.EntityContent
	for _, e := range s.entities {
		if e.EntityType == entityType && e.EntityName == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// notFoundFixtureEntities holds two granted (repo-a) and one ungranted
// (repo-b) deployment records for "payments", plus a wrong-type and a
// wrong-name distractor that must never surface as references.
func notFoundFixtureEntities() []querycontract.EntityContent {
	return []querycontract.EntityContent{
		{RepoID: "repo-b", RelativePath: "gitops/app.yaml", EntityType: "ArgoCDApplication", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "charts/payments/Chart.yaml", EntityType: "HelmChart", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "apps/payments.yaml", EntityType: "ArgoCDApplicationSet", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "deploy/svc.yaml", EntityType: "Deployment", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "gitops/other.yaml", EntityType: "ArgoCDApplication", EntityName: "other"},
	}
}

func notFoundChainHandler(store querycontract.ContentStore) *Handler {
	return &Handler{TraceContext: nilImpactTraceContext{}, Content: store, Profile: querycontract.ProfileProduction}
}

// postImpactEnvelope serves one envelope-negotiated request through Mount.
func postImpactEnvelope(t *testing.T, h *Handler, path, body string, authCtx *auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeNotFoundEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Error struct {
			Code       string         `json:"code"`
			Message    string         `json:"message"`
			Capability string         `json:"capability"`
			Details    map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body is not an error envelope: %v; body = %s", err, rec.Body.String())
	}
	out := map[string]any{
		"code":       envelope.Error.Code,
		"message":    envelope.Error.Message,
		"capability": envelope.Error.Capability,
		"details":    envelope.Error.Details,
	}
	return out
}

// TestTraceDeploymentChainNotFoundIsTypedUnderEnvelope pins the Gap 3 typed
// refusal: an envelope client gets code service_not_found with
// details.references carrying the granted deployment records, sorted, as
// file handles that never name a workload id.
func TestTraceDeploymentChainNotFoundIsTypedUnderEnvelope(t *testing.T) {
	t.Parallel()

	rec := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities()}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeNotFoundEnvelope(t, rec)
	if got["code"] != "service_not_found" {
		t.Errorf("code = %v, want service_not_found; body = %s", got["code"], rec.Body.String())
	}
	if got["message"] != "service not found" {
		t.Errorf("message = %v, want %q; body = %s", got["message"], "service not found", rec.Body.String())
	}
	if got["capability"] != "platform_impact.deployment_chain" {
		t.Errorf("capability = %v, want platform_impact.deployment_chain; body = %s", got["capability"], rec.Body.String())
	}
	details, _ := got["details"].(map[string]any)
	wantRefs := []any{
		map[string]any{"repo_id": "repo-a", "relative_path": "apps/payments.yaml", "entity_type": "ArgoCDApplicationSet", "entity_name": "payments"},
		map[string]any{"repo_id": "repo-a", "relative_path": "charts/payments/Chart.yaml", "entity_type": "HelmChart", "entity_name": "payments"},
		map[string]any{"repo_id": "repo-b", "relative_path": "gitops/app.yaml", "entity_type": "ArgoCDApplication", "entity_name": "payments"},
	}
	if !reflect.DeepEqual(details["references"], wantRefs) {
		t.Errorf("references = %v, want %v", details["references"], wantRefs)
	}
	if truncated, _ := details["truncated"].(bool); truncated {
		t.Errorf("truncated = true, want false; details = %v", details)
	}
	if strings.Contains(rec.Body.String(), "workload:") {
		t.Errorf("body names a workload id: %s", rec.Body.String())
	}
}

// TestTraceDeploymentChainNotFoundPlainBodyByteIdentical pins the plain
// contract: without the envelope Accept header the typed refusal renders
// exactly the historical bare 404.
func TestTraceDeploymentChainNotFoundPlainBodyByteIdentical(t *testing.T) {
	t.Parallel()

	rec := serveImpactRequest(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities()}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	const want = "{\"detail\":\"service not found\",\"error\":\"Not Found\"}\n"
	if rec.Body.String() != want {
		t.Errorf("plain body = %q, want %q", rec.Body.String(), want)
	}
}

// TestTraceDeploymentChainNotFoundUngrantedRendersAsUnknown pins the hard
// constraint: a scoped caller whose grant admits none of the matching
// deployment records gets a body byte-identical to an unknown name, with no
// cross-tenant strings.
func TestTraceDeploymentChainNotFoundUngrantedRendersAsUnknown(t *testing.T) {
	t.Parallel()

	a := tenantAAuth()
	// Only tenant B holds a same-name deployment record, so tenant A is
	// ungranted for every hint the probe could return.
	ungrantedEntities := []querycontract.EntityContent{
		{RepoID: "repo-b", RelativePath: "gitops/app.yaml", EntityType: "ArgoCDApplication", EntityName: "payments"},
	}
	ungranted := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{entities: ungrantedEntities}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, &a)
	unknown := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, &a)
	// The same scoped caller does see the records its own grant admits.
	granted := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities()}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, &a)
	if ungranted.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound {
		t.Fatalf("status = %d/%d, want 404/404; bodies = %s / %s",
			ungranted.Code, unknown.Code, ungranted.Body.String(), unknown.Body.String())
	}
	if ungranted.Body.String() != unknown.Body.String() {
		t.Errorf("ungranted body\n %s\ndiffers from unknown body\n %s",
			ungranted.Body.String(), unknown.Body.String())
	}
	assertNoTenantB(t, ungranted.Body.String())
	grantedGot := decodeNotFoundEnvelope(t, granted)
	grantedDetails, _ := grantedGot["details"].(map[string]any)
	grantedRefs, _ := grantedDetails["references"].([]any)
	if len(grantedRefs) != 2 {
		t.Errorf("granted references holds %d handles, want the 2 repo-a records; details = %v",
			len(grantedRefs), grantedDetails)
	}
}

// TestTraceDeploymentChainNotFoundEmptyGrantProbesNothing pins the fail-closed
// floor: an empty grant answers the zero-evidence shape without issuing any
// content probe.
func TestTraceDeploymentChainNotFoundEmptyGrantProbesNothing(t *testing.T) {
	t.Parallel()

	var probes atomic.Int64
	empty := testutil.ScopedTestAuthContext("tenant-a", nil)
	rec := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities(), probes: &probes}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, &empty)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	if n := probes.Load(); n != 0 {
		t.Errorf("empty grant issued %d content probes, want 0", n)
	}
	unknown := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{}),
		"/api/v0/impact/trace-deployment-chain", `{"service_name":"payments"}`, &empty)
	if rec.Body.String() != unknown.Body.String() {
		t.Errorf("empty-grant body\n %s\ndiffers from unknown body\n %s",
			rec.Body.String(), unknown.Body.String())
	}
}

// TestDeploymentConfigInfluenceNotFoundIsTypedUnderEnvelope pins the second
// branch of the shared Gap 3 helper: the config-influence route answers the
// same typed refusal under envelope accept and the same bare bytes for plain
// clients.
func TestDeploymentConfigInfluenceNotFoundIsTypedUnderEnvelope(t *testing.T) {
	t.Parallel()

	rec := postImpactEnvelope(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities()}),
		"/api/v0/impact/deployment-config-influence", `{"service_name":"payments"}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeNotFoundEnvelope(t, rec)
	if got["code"] != "service_not_found" {
		t.Errorf("code = %v, want service_not_found; body = %s", got["code"], rec.Body.String())
	}
	if got["capability"] != "platform_impact.deployment_config_influence" {
		t.Errorf("capability = %v, want platform_impact.deployment_config_influence; body = %s",
			got["capability"], rec.Body.String())
	}
	details, _ := got["details"].(map[string]any)
	refs, _ := details["references"].([]any)
	if len(refs) != 3 {
		t.Errorf("references holds %d handles, want 3; details = %v", len(refs), details)
	}

	plain := requestDeploymentConfigInfluence(t,
		notFoundChainHandler(exactNameContentStub{entities: notFoundFixtureEntities()}),
		`{"service_name":"payments"}`)
	if plain.Code != http.StatusNotFound {
		t.Fatalf("plain status = %d, want 404; body = %s", plain.Code, plain.Body.String())
	}
	const want = "{\"detail\":\"service not found\",\"error\":\"Not Found\"}\n"
	if plain.Body.String() != want {
		t.Errorf("plain body = %q, want %q", plain.Body.String(), want)
	}
}
