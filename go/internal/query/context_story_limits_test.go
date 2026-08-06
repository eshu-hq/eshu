// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetWorkloadContextReturnsResultLimitsAndPartialReasons proves the
// workload context HTTP route carries the additive result_limits drilldown
// block and an explicit partial_reasons slot alongside the truth envelope.
func TestGetWorkloadContextReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/workloads/workload-1/context", "workload_id", "workload-1")
	requireContextResultLimits(t, data, "get_workload_story", "/api/v0/workloads/workload-1/context")
}

// TestGetWorkloadStoryReturnsResultLimitsAndPartialReasons proves the workload
// story HTTP route carries the same hardened metadata, with the drilldown tool
// pointing back at the workload context route.
func TestGetWorkloadStoryReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: workloadEnvelopeGraphReader("workload-1", "order-service"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/workloads/workload-1/story", "workload_id", "workload-1")
	requireContextResultLimits(t, data, "get_workload_context", "/api/v0/workloads/workload-1/context")
}

// TestGetEntityContextReturnsResultLimitsAndPartialReasons proves the entity
// context HTTP route carries the hardened result_limits drilldown block and an
// explicit partial_reasons slot alongside the truth envelope.
func TestGetEntityContextReturnsResultLimitsAndPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "WHERE e.id = $entity_id") {
					t.Fatalf("RunSingle cypher = %q, want entity lookup", cypher)
				}
				return map[string]any{
					"id":            "entity-1",
					"labels":        []any{"Function"},
					"name":          "handler",
					"file_path":     "src/handler.go",
					"repo_id":       "repo-1",
					"repo_name":     "repo-1",
					"language":      "go",
					"start_line":    int64(12),
					"end_line":      int64(24),
					"relationships": []any{},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	data := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	requireContextResultLimits(t, data, "get_relationship_evidence", "/api/v0/entities/entity-1/context")
}

func TestGetEntityContextHTTPDeterministicallyCapsRelationships(t *testing.T) {
	t.Parallel()

	relationships := make([]any, contextStoryItemLimit+10)
	for i := range relationships {
		index := len(relationships) - i - 1
		relationships[i] = map[string]any{
			"type":        "DEPENDS_ON",
			"target_name": fmt.Sprintf("target-%03d", index),
			"target_id":   fmt.Sprintf("repository:%03d", index),
		}
	}
	handler := &EntityHandler{Neo4j: fakeGraphReader{runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
		return map[string]any{
			"id":            "entity-1",
			"labels":        []any{"File"},
			"name":          "ci",
			"file_path":     ".github/workflows/ci.yml",
			"repo_id":       "repo-1",
			"relationships": relationships,
		}, nil
	}}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	first := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	second := contextEnvelopeHTTPData(t, mux, "/api/v0/entities/entity-1/context", "entity_id", "entity-1")
	for run, data := range []map[string]any{first, second} {
		rows := mapSliceValue(data, "relationships")
		if got, want := len(rows), contextStoryItemLimit; got != want {
			t.Fatalf("run %d relationship payload len = %d, want %d", run, got, want)
		}
		limits := mapValue(data, "result_limits")
		if got, want := IntVal(limits, "relationship_count"), contextStoryItemLimit+10; got != want {
			t.Fatalf("run %d relationship_count = %d, want %d", run, got, want)
		}
		if truncated, _ := limits["truncated"].(bool); !truncated {
			t.Fatalf("run %d result_limits.truncated = false, want true", run)
		}
		if got, want := StringVal(rows[0], "target_name"), "target-000"; got != want {
			t.Fatalf("run %d first target = %q, want %q", run, got, want)
		}
		if got, want := StringVal(rows[len(rows)-1], "target_name"), "target-049"; got != want {
			t.Fatalf("run %d last target = %q, want %q", run, got, want)
		}
	}
}

// TestWorkloadContextResultLimitsCapsFanoutInPlace proves the workload limits
// block caps each fan-out slice to the stated limit and only then reports
// truncated, so the payload never exceeds limit while claiming truncation.
func TestWorkloadContextResultLimitsCapsFanoutInPlace(t *testing.T) {
	t.Parallel()

	instances := make([]any, contextStoryItemLimit+10)
	for i := range instances {
		instances[i] = map[string]any{"id": i}
	}
	ctx := map[string]any{"instances": instances}

	limits := workloadContextResultLimits(ctx, "workload-1", "context")

	if got := len(mapSliceValue(ctx, "instances")); got != contextStoryItemLimit {
		t.Fatalf("instances payload len = %d, want capped to %d", got, contextStoryItemLimit)
	}
	if got, want := limits["instance_count"], contextStoryItemLimit+10; got != want {
		t.Fatalf("result_limits.instance_count = %v, want total %d", got, want)
	}
	if truncated, _ := limits["truncated"].(bool); !truncated {
		t.Fatal("result_limits.truncated = false, want true when fan-out exceeds the limit")
	}
}

// TestWorkloadContextResultLimitsNotTruncatedUnderLimit proves a workload whose
// fan-out is within the limit is not falsely marked truncated.
func TestWorkloadContextResultLimitsNotTruncatedUnderLimit(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"instances": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}}

	limits := workloadContextResultLimits(ctx, "workload-1", "context")

	if truncated, _ := limits["truncated"].(bool); truncated {
		t.Fatal("result_limits.truncated = true, want false when fan-out is within the limit")
	}
	if got := len(mapSliceValue(ctx, "instances")); got != 2 {
		t.Fatalf("instances payload len = %d, want 2 unchanged", got)
	}
}

// TestWorkloadContextResultLimitsReflectsUpstreamTruncationFlags is the PR
// #5933 review fix (Codex, service_query_enrichment.go:190).
// service_query_enrichment.go sets dependents_truncated,
// consumer_repositories_truncated, and provisioning_source_chains_truncated on
// the workload context whenever the provisioning-candidate, evidence-file,
// hostname, or consumer-search reads underneath those lists hit their own
// bound -- a bound well below contextStoryItemLimit (50). Before this fix,
// workloadContextResultLimits only ORed the in-place fan-out caps, so a
// caller reading result_limits.truncated on GET
// /api/v0/workloads/{id}/context or /story could see false next to a true
// dependents_truncated: the exact false-complete signal these flags exist to
// prevent (docs/public/reference/http-api/context-and-stories.md), and the
// same signal buildServiceResultLimitsWithContext (service_story_dossier.go)
// already ORs in for the service story route.
func TestWorkloadContextResultLimitsReflectsUpstreamTruncationFlags(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{
		"dependents_truncated",
		"consumer_repositories_truncated",
		"provisioning_source_chains_truncated",
	} {
		t.Run(flag, func(t *testing.T) {
			ctx := map[string]any{
				"dependents": []any{map[string]any{"id": 1}, map[string]any{"id": 2}},
				flag:         true,
			}

			limits := workloadContextResultLimits(ctx, "workload-1", "context")

			if truncated, _ := limits["truncated"].(bool); !truncated {
				t.Fatalf(
					"result_limits.truncated = false with %s = true, want true (a bounded upstream read below contextStoryItemLimit still truncated the underlying evidence)",
					flag,
				)
			}
		})
	}
}

// TestWorkloadContextResultLimitsNotTruncatedWithoutUpstreamFlags proves a
// workload with no in-place cap and no upstream truncation flag is not
// falsely marked truncated by the fix above.
func TestWorkloadContextResultLimitsNotTruncatedWithoutUpstreamFlags(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"dependents":            []any{map[string]any{"id": 1}},
		"consumer_repositories": []any{map[string]any{"id": 2}},
	}

	limits := workloadContextResultLimits(ctx, "workload-1", "context")

	if truncated, _ := limits["truncated"].(bool); truncated {
		t.Fatal("result_limits.truncated = true, want false with no in-place cap and no upstream truncation flag")
	}
}

func contextEnvelopeHTTPData(t *testing.T, mux *http.ServeMux, path, pathKey, pathValue string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue(pathKey, pathValue)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	if envelope.Truth == nil {
		t.Fatal("truth is nil, want context truth envelope")
	}
	return repositoryEnvelopeData(t, envelope)
}

func requireContextResultLimits(t *testing.T, data map[string]any, wantTool, wantPath string) {
	t.Helper()

	limits, ok := data["result_limits"].(map[string]any)
	if !ok || len(limits) == 0 {
		t.Fatalf("data.result_limits missing, want bounded drilldown block; data = %#v", data)
	}
	if limits["limit"] == nil {
		t.Fatal("result_limits.limit missing, want bounded limit")
	}
	if got, want := StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if _, ok := limits["truncated"].(bool); !ok {
		t.Fatalf("result_limits.truncated type = %T, want bool", limits["truncated"])
	}
	if got, want := StringVal(limits, "drilldown_tool"), wantTool; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := StringVal(limits, "context_path"), wantPath; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}
	if _, ok := data["partial_reasons"]; !ok {
		t.Fatal("data.partial_reasons missing, want explicit partial reason slot")
	}
}

// infrastructureOverflowContentStore is a ContentStore double for
// TestGetWorkloadContextAndStoryResultLimitsReflectInfrastructureTruncated
// below. It embeds fakePortContentStore's zero-value (empty) behavior for
// every method the service-context enrichment pipeline calls besides
// ListRepoEntitiesByTypes -- all safe no-op reads, since this test seeds no
// files, entities, or hostnames for them to find -- and overrides ONLY
// ListRepoEntitiesByTypes, the type-filtered read
// queryRepoInfrastructureFromContent (repository_infrastructure.go) actually
// calls (P1 review follow-up to #5764; the untyped ListRepoEntities is no
// longer on this path). Unlike fakePortContentStore's own
// ListRepoEntitiesByTypes (which self-clamps to the caller's limit, mirroring
// a real SQL LIMIT), this override ignores the type list and limit arguments
// and returns every seeded row unconditionally, so the production code's own
// `len(entities) > repositoryInfrastructureEntityLimit` check -- not a
// client-side fake clamp -- is what decides truncation.
type infrastructureOverflowContentStore struct {
	fakePortContentStore
	infrastructureEntities []EntityContent
}

func (s infrastructureOverflowContentStore) ListRepoEntitiesByTypes(_ context.Context, _ string, _ []string, _ int) ([]EntityContent, error) {
	return s.infrastructureEntities, nil
}

func overflowingInfrastructureEntities(n int) []EntityContent {
	entities := make([]EntityContent, 0, n)
	for i := 0; i < n; i++ {
		entities = append(entities, EntityContent{
			RepoID:       "repo-1",
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("resource-%05d", i),
			RelativePath: fmt.Sprintf("deploy/resource-%05d.yaml", i),
		})
	}
	return entities
}

// TestGetWorkloadContextAndStoryResultLimitsReflectInfrastructureTruncated is
// the PR #5933 review follow-up: workloadContextResultLimits ORs
// dependents_truncated, consumer_repositories_truncated, and
// provisioning_source_chains_truncated, but did not read ctx["limitations"]
// for infrastructure_truncated, even though fetchWorkloadContextForOperation
// (entity_workload_context.go) appends that reason when the repository's
// infrastructure-entity read hits repositoryInfrastructureEntityLimit. This
// drives a genuine repositoryInfrastructureEntityLimit+1-row overflow through
// the real mounted /api/v0/workloads/{id}/context and /story routes -- with no
// dependents, consumer, or provisioning-source-chain overflow at all, so
// infrastructure_truncated is the ONLY bound that fires -- through
// handler.Mount(mux) and mux.ServeHTTP, decoding the actual JSON wire body, and
// proves result_limits.truncated reflects it.
func TestGetWorkloadContextAndStoryResultLimitsReflectInfrastructureTruncated(t *testing.T) {
	t.Parallel()

	content := infrastructureOverflowContentStore{
		infrastructureEntities: overflowingInfrastructureEntities(repositoryInfrastructureEntityLimit + 1),
	}
	// fetchWorkloadRepositoryForAccess (entity_workload_context.go) re-resolves
	// repo_id through its own DEFINES-anchored candidate query before the
	// infrastructure read runs; workloadEnvelopeGraphReader's default runByMatch
	// table has no entry for it, which would leave repoID empty and skip the
	// infrastructure fetch entirely. A custom run func answers only that exact
	// query (matched on its distinctive RETURN clause) and leaves every other
	// query -- topology, dependencies, provisioning candidates, the graph
	// infrastructure fallback -- returning empty, so infrastructure_truncated
	// stays the only bound this test can fire.
	graphReader := fakeWorkloadGraphReader{
		runSingleByMatch: map[string]map[string]any{
			"MATCH (w:Workload)": {
				"id": "workload-1", "name": "order-service", "kind": "Deployment",
				"repo_id": "repo-1", "repo_name": "order-service", "instances": []any{},
			},
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN DISTINCT r.id as repo_id, r.name as repo_name") {
				return []map[string]any{{"repo_id": "repo-1", "repo_name": "order-service"}}, nil
			}
			return nil, nil
		},
	}

	for _, path := range []string{
		"/api/v0/workloads/workload-1/context",
		"/api/v0/workloads/workload-1/story",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			handler := &EntityHandler{
				Neo4j:   graphReader,
				Content: content,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.SetPathValue("workload_id", "workload-1")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error = %v", err)
			}
			// The route legitimately returns up to repositoryInfrastructureEntityLimit
			// raw infrastructure rows; the response body is large by design and left
			// out of failure messages below so a failing run stays readable.
			limits := mapValue(body, "result_limits")
			partialReasons := StringSliceVal(body, "partial_reasons")
			if !strings.Contains(strings.Join(partialReasons, ","), "infrastructure_truncated") {
				t.Fatalf(
					"partial_reasons = %v, want it to contain %q (the seeded overflow must reach ctx[\"limitations\"] before result_limits can ever reflect it)",
					partialReasons, "infrastructure_truncated",
				)
			}
			if truncated, _ := limits["truncated"].(bool); !truncated {
				t.Fatalf(
					"result_limits.truncated = %#v, want true (a genuine %d-row infrastructure overflow with no other bound firing, and partial_reasons already contains infrastructure_truncated: %v)",
					limits["truncated"], repositoryInfrastructureEntityLimit+1, partialReasons,
				)
			}
		})
	}
}
