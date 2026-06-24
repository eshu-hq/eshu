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
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
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
// the present/present fixture used by query.compare_test.go but lives in the mcp
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
