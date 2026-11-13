// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Answer parity workflow tests (issue #1795).
//
// Each test drives ONE logical question through the HTTP surface and the MCP
// surface against the SAME fixture handler, then asserts the canonical envelope
// fields agree. Workflows covered:
//
//   - Environment comparison (compare_environments): exact present/present case
//     and the profile-gated unsupported case.
//   - Repository investigation (get_repo_story): exact context-overview case.
//
// Cases exercise exact and unsupported behavior; the compare fixture also proves
// the inferred/missing-evidence markers stay equal across surfaces. The MCP
// #1791 text summary is asserted as convenience only — it never replaces the
// structured-content assertions.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// TestEnvironmentCompareAnswerParityExact proves the present/present environment
// comparison answer agrees across the HTTP and MCP surfaces on truth, freshness,
// result limits, evidence handles, and missing-evidence behavior.
func TestEnvironmentCompareAnswerParityExact(t *testing.T) {
	t.Parallel()

	handler := mountCompareHandler(t, query.ProfileProduction, presentBothCompareReader())
	args := map[string]any{
		"workload_id": "workload:service-edge-api",
		"left":        "qa",
		"right":       "prod",
	}
	body := map[string]any{
		"workload_id": "workload:service-edge-api",
		"left":        "qa",
		"right":       "prod",
		"limit":       50,
	}

	httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/compare/environments", body)
	mcpEnv, summary := mcpEnvelope(t, handler, "compare_environments", args)

	httpCmp := extractComparable(t, httpEnv)
	mcpCmp := extractComparable(t, mcpEnv)
	requireParity(t, "http", "mcp", httpCmp, mcpCmp)

	// Anchor the shared contract so a future regression cannot make both
	// surfaces wrong in lockstep and still "pass" parity.
	if got, want := httpCmp.truthCapability, "platform_impact.environment_compare"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := httpCmp.truthBasis, query.TruthBasisHybrid; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}
	if got, want := httpCmp.freshnessState, query.FreshnessFresh; got != want {
		t.Fatalf("freshness = %q, want %q", got, want)
	}
	if len(httpCmp.evidenceHandles) == 0 {
		t.Fatal("evidence handles empty, want cited cloud resources on both sides")
	}
	if len(httpCmp.missingEvidence) != 0 {
		t.Fatalf("missing evidence = %v, want none for present/present", httpCmp.missingEvidence)
	}

	// #1791 convenience: the text summary must be present and lead with truth,
	// but the structured envelope above remains the source of truth.
	requireConvenienceSummary(t, summary, mcpEnv)
}

// TestEnvironmentCompareAnswerParityUnsupported proves that when the runtime
// profile cannot support environment comparison, BOTH surfaces return the same
// unsupported_capability error envelope and neither fabricates a confident
// answer.
func TestEnvironmentCompareAnswerParityUnsupported(t *testing.T) {
	t.Parallel()

	// local_lightweight has no support row for environment_compare, so the
	// handler must refuse rather than invent an answer.
	handler := mountCompareHandler(t, query.ProfileLocalLightweight, presentBothCompareReader())
	args := map[string]any{
		"workload_id": "workload:service-edge-api",
		"left":        "qa",
		"right":       "prod",
	}
	body := map[string]any{
		"workload_id": "workload:service-edge-api",
		"left":        "qa",
		"right":       "prod",
	}

	httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/compare/environments", body)
	mcpEnv, summary := mcpEnvelope(t, handler, "compare_environments", args)

	httpCmp := extractComparable(t, httpEnv)
	mcpCmp := extractComparable(t, mcpEnv)
	requireParity(t, "http", "mcp", httpCmp, mcpCmp)

	if !httpCmp.hasError {
		t.Fatal("http surface returned a success answer, want unsupported_capability error")
	}
	if got, want := httpCmp.errorCode, query.ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := httpCmp.errorCapability, "platform_impact.environment_compare"; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
	// Neither surface may leak a confident truth level on the unsupported path.
	if httpCmp.truthLevel != "" || mcpCmp.truthLevel != "" {
		t.Fatalf("unsupported answer leaked truth level: http=%q mcp=%q", httpCmp.truthLevel, mcpCmp.truthLevel)
	}

	// The convenience summary must surface the error code, not a false success.
	if !strings.Contains(summary, string(query.ErrorCodeUnsupportedCapability)) {
		t.Fatalf("summary = %q, want it to surface %q", summary, query.ErrorCodeUnsupportedCapability)
	}
}

// TestRepositoryInvestigationAnswerParityExact proves the repository story
// answer (a repository/code-topic investigation) agrees across the HTTP and MCP
// surfaces on truth and capability for the same fixture repository.
//
// Deliberate split premise (#10605): HTTP defaults to full while the MCP tool
// defaults to handles, so this test anchors only the fields that must agree
// across the split (truth, capability, freshness). The body-level split is
// pinned field-for-field by
// TestRepositoryInvestigationAnswerParityHandlesSplit.
func TestRepositoryInvestigationAnswerParityExact(t *testing.T) {
	t.Parallel()

	handler := mountRepositoryHandler(t, "repo-parity")

	httpEnv := httpEnvelope(t, handler, http.MethodGet, "/api/v0/repositories/repo-parity/story", nil)
	mcpEnv, summary := mcpEnvelope(t, handler, "get_repo_story", map[string]any{"repo_id": "repo-parity"})

	httpCmp := extractComparable(t, httpEnv)
	mcpCmp := extractComparable(t, mcpEnv)
	requireParity(t, "http", "mcp", httpCmp, mcpCmp)

	if got, want := httpCmp.truthCapability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if httpCmp.hasError {
		t.Fatalf("http repo story returned error envelope: %+v", httpEnv.Error)
	}
	requireConvenienceSummary(t, summary, mcpEnv)
}

// TestRepositoryInvestigationAnswerParityHandlesSplit pins the intended #10605
// default split field-for-field on a populated story: the MCP default answer
// is byte-identical to HTTP ?evidence_detail=handles on data and on truth
// omissions, and both differ from the HTTP default (full) on the shaped
// families. The relationships cut must be real (non-empty omissions with the
// rows the story held), so a no-op shaper cannot pass this test.
func TestRepositoryInvestigationAnswerParityHandlesSplit(t *testing.T) {
	t.Parallel()

	handler := mountPopulatedRepositoryHandler(t, "repo-parity")

	httpFull := httpEnvelope(t, handler, http.MethodGet, "/api/v0/repositories/repo-parity/story", nil)
	httpHandles := httpEnvelope(t, handler, http.MethodGet, "/api/v0/repositories/repo-parity/story?evidence_detail=handles", nil)
	mcpEnv, _ := mcpEnvelope(t, handler, "get_repo_story", map[string]any{"repo_id": "repo-parity"})

	if got := stringVal(mcpEnv.Data, "evidence_detail"); got != "handles" {
		t.Fatalf("mcp evidence_detail = %q, want handles (the MCP default)", got)
	}
	if got := stringVal(httpHandles.Data, "evidence_detail"); got != "handles" {
		t.Fatalf("http handles evidence_detail = %q, want handles", got)
	}

	mcpData, err := json.Marshal(mcpEnv.Data)
	if err != nil {
		t.Fatalf("marshal mcp data: %v", err)
	}
	handlesData, err := json.Marshal(httpHandles.Data)
	if err != nil {
		t.Fatalf("marshal http handles data: %v", err)
	}
	if string(mcpData) != string(handlesData) {
		t.Fatal("mcp default data differs from http ?evidence_detail=handles data; want field-for-field agreement")
	}
	fullData, err := json.Marshal(httpFull.Data)
	if err != nil {
		t.Fatalf("marshal http full data: %v", err)
	}
	if string(mcpData) == string(fullData) {
		t.Fatal("mcp default data equals http default (full) data; want the handles split to be real on a populated story")
	}

	mcpOmissions, err := json.Marshal(mcpEnv.Truth.Omissions)
	if err != nil {
		t.Fatalf("marshal mcp omissions: %v", err)
	}
	handlesOmissions, err := json.Marshal(httpHandles.Truth.Omissions)
	if err != nil {
		t.Fatalf("marshal http handles omissions: %v", err)
	}
	if string(mcpOmissions) != string(handlesOmissions) {
		t.Fatal("mcp truth omissions differ from http handles truth omissions; want the same named cuts")
	}
	found := false
	for _, omission := range mcpEnv.Truth.Omissions {
		if omission.Section == "relationship_overview.relationships" {
			found = true
			if omission.Total != 2 {
				t.Fatalf("relationships omission total = %d, want the 2 rows the story held", omission.Total)
			}
		}
	}
	if !found {
		t.Fatal("no relationship_overview.relationships omission; want a real cut, not an empty fixture")
	}
}

// stringVal reads a top-level string field from envelope data without
// importing the querycontract helpers into this package's test surface.
func stringVal(data any, key string) string {
	m, _ := data.(map[string]any)
	s, _ := m[key].(string)
	return s
}

// mountPopulatedRepositoryHandler mounts a RepositoryHandler whose fixture
// graph carries a base repository row plus two dependency edges, so the story
// has a relationship family for handles to cut.
func mountPopulatedRepositoryHandler(t *testing.T, repoID string) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.RepositoryHandler{
		Neo4j:   populatedRepoReader{repoID: repoID},
		Content: content.FakePortContentStore{},
	}
	handler.Mount(mux)
	return mux
}

// populatedRepoReader is a deterministic repository-story fixture: a single
// repository identity, bounded coverage counts, and two outgoing dependency
// edges, mirroring the handler-level evidence_detail fixture so the handles
// cut is real.
type populatedRepoReader struct {
	repoID string
}

func (r populatedRepoReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		return nil, nil
	}
	return map[string]any{
		"id": r.repoID, "name": "edge-api", "path": "/repos/edge-api",
		"local_path": "/repos/edge-api", "repo_slug": "acme/edge-api",
		"remote_url": "https://github.com/acme/edge-api.git", "has_remote": true,
	}, nil
}

func (r populatedRepoReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
		return []map[string]any{{"count": int64(3)}}, nil
	case strings.Contains(cypher, "RETURN f.language AS language, count(DISTINCT f) AS file_count"):
		return []map[string]any{{"language": "go", "file_count": int64(3)}}, nil
	case strings.Contains(cypher, "DEPENDS_ON"):
		return []map[string]any{
			{
				"type": "DEPLOYS_FROM", "target_name": "platform", "target_id": "repo-platform",
				"evidence_type": "argocd_application", "resolved_id": "res-1",
				"confidence": 0.9, "evidence_count": 3,
			},
			{
				"type": "DEPENDS_ON", "target_name": "lib", "target_id": "repo-lib",
				"evidence_type": "go_mod_require",
			},
		}, nil
	default:
		return nil, nil
	}
}

// requireConvenienceSummary asserts the #1791 text summary is present and
// faithful to the envelope's truth/error WITHOUT being treated as canonical: the
// structured envelope is still asserted independently by the caller.
func requireConvenienceSummary(t *testing.T, summary string, env *query.ResponseEnvelope) {
	t.Helper()

	if strings.TrimSpace(summary) == "" {
		t.Fatal("text summary empty, want bounded convenience summary")
	}
	if len(summary) > maxSummaryLength {
		t.Fatalf("summary length = %d, want <= %d", len(summary), maxSummaryLength)
	}
	// A successful envelope's summary must lead with the truth level so the
	// human convenience text never claims more confidence than the structured
	// truth supports.
	if env.Error == nil && env.Truth != nil && env.Truth.Level != "" {
		if !strings.Contains(summary, string(env.Truth.Level)) {
			t.Fatalf("summary = %q, want it to surface truth level %q", summary, env.Truth.Level)
		}
	}
}

// mountCompareHandler mounts a CompareHandler with the given profile and graph
// reader on a fresh mux, returning the handler both surfaces share.
func mountCompareHandler(t *testing.T, profile query.QueryProfile, reader query.GraphQuery) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.CompareHandler{Neo4j: reader, Profile: profile}
	handler.Mount(mux)
	return mux
}

// mountRepositoryHandler mounts a RepositoryHandler backed by a fixture graph
// reader returning a single repository identity for repo story / stats routes.
func mountRepositoryHandler(t *testing.T, repoID string) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.RepositoryHandler{Neo4j: parityRepoReader{repoID: repoID}}
	handler.Mount(mux)
	return mux
}

// presentBothCompareReader returns a graph reader where both environments
// materialize a workload instance with one cited cloud resource each, producing
// a present/present comparison with stable evidence handles.
func presentBothCompareReader() query.GraphQuery {
	return parityCompareReader{}
}

// parityCompareReader is a deterministic compare-environments fixture. It mirrors
// the present/present fixture used by internal/query/compare/handler_test.go but lives in the mcp
// package so both surfaces can be driven from one handler instance.
type parityCompareReader struct{}

func (parityCompareReader) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	switch {
	case strings.Contains(cypher, "MATCH (w:Workload)"):
		return map[string]any{
			"id":      "workload:service-edge-api",
			"name":    "service-edge-api",
			"kind":    "service",
			"repo_id": "repo-service-edge-api",
		}, nil
	case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
		env, _ := params["environment"].(string)
		if env == "" {
			return nil, nil
		}
		return map[string]any{
			"id":          "instance:" + env,
			"name":        "service-edge-api-" + env,
			"kind":        "service",
			"environment": env,
			"workload_id": "workload:service-edge-api",
		}, nil
	}
	return nil, nil
}

func (parityCompareReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)") {
		return nil, nil
	}
	instance, _ := params["instance_id"].(string)
	switch instance {
	case "instance:qa":
		return []map[string]any{{
			"id":         "cloud:queue-qa",
			"name":       "queue-qa",
			"kind":       "queue",
			"provider":   "aws",
			"confidence": 1.0,
			"reason":     "materialized_cloud_dependency",
		}}, nil
	case "instance:prod":
		return []map[string]any{{
			"id":         "cloud:queue-prod",
			"name":       "queue-prod",
			"kind":       "queue",
			"provider":   "aws",
			"confidence": 0.8,
			"reason":     "materialized_cloud_dependency",
		}}, nil
	}
	return nil, nil
}

// parityRepoReader is a deterministic repository-story fixture returning a single
// repository identity plus empty enrichment fan-out, matching the shape the
// RepositoryHandler expects.
type parityRepoReader struct {
	repoID string
}

func (r parityRepoReader) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return map[string]any{
		"id":         r.repoID,
		"name":       "edge-api",
		"path":       "/repos/edge-api",
		"local_path": "/repos/edge-api",
		"has_remote": false,
	}, nil
}

func (parityRepoReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
		return []map[string]any{{"count": int64(3)}}, nil
	case strings.Contains(cypher, "RETURN f.language AS language, count(DISTINCT f) AS file_count"):
		return []map[string]any{{"language": "go", "file_count": int64(3)}}, nil
	default:
		return nil, nil
	}
}
