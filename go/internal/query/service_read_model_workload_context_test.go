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

func TestGetServiceContextFallsBackToRepositoryWorkloadIdentity(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo-serverless",
				Name: "serverless-job",
				Path: "/repos/serverless-job",
			}},
			summary: repositoryReadModelSummary{
				Available:     true,
				WorkloadNames: []string{"serverless-job"},
			},
			entities: []EntityContent{
				{
					RepoID:       "repo-serverless",
					RelativePath: "template.yml",
					EntityType:   "CloudFormationResource",
					EntityName:   "ProcessRecords",
					Language:     "yaml",
					Metadata: map[string]any{
						"resource_type": "AWS::Serverless::Function",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/serverless-job/context", nil)
	req.SetPathValue("service_name", "serverless-job")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["id"], "workload:serverless-job"; got != want {
		t.Fatalf("id = %#v, want %#v", got, want)
	}
	if got, want := resp["materialization_status"], "identity_only"; got != want {
		t.Fatalf("materialization_status = %#v, want %#v", got, want)
	}
	deploymentEvidence := mapValue(resp, "deployment_evidence")
	familyPaths := mapSliceValue(deploymentEvidence, "delivery_family_paths")
	cloudFormation := requireRepositoryStoryDeliveryFamily(familyPaths, "cloudformation")
	if cloudFormation == nil {
		t.Fatalf("delivery_family_paths = %#v, want cloudformation family", familyPaths)
	}
	if got, want := cloudFormation["mode"], "serverless_delivery"; got != want {
		t.Fatalf("cloudformation.mode = %#v, want %#v", got, want)
	}
}

// nonFilteringInfrastructureContentStore models a ContentStore whose
// ListRepoEntitiesByTypes does not honor the requested entity_type set (a
// pathological implementation, or a future content-store backend that drifts
// from the Postgres ANY($types) contract) and instead returns rawEntities
// verbatim up to limit. It exists to reach the
// truncated=true/zero-infrastructure-rows combination
// TestGetServiceContextReadModelResetsTruncatedOnGraphFallbackError needs at
// the entity_workload_context.go read-model call site (#5764 P2 review
// follow-up): fetchServiceReadModelWorkloadContext's reset of
// infrastructureTruncated on a graph-fallback error must hold as an
// invariant of that call site's OWN logic, not only as an accidental
// side effect of queryRepoInfrastructureFromContent's own type filter
// (fixed separately, P1 review follow-up).
type nonFilteringInfrastructureContentStore struct {
	fakePortContentStore
	rawEntities []EntityContent
}

func (s nonFilteringInfrastructureContentStore) ListRepoEntitiesByTypes(_ context.Context, _ string, _ []string, limit int) ([]EntityContent, error) {
	if limit > 0 && limit < len(s.rawEntities) {
		return append([]EntityContent(nil), s.rawEntities[:limit]...), nil
	}
	return append([]EntityContent(nil), s.rawEntities...), nil
}

// TestGetServiceContextReadModelResetsTruncatedOnGraphFallbackError is the
// regression test for the #5764 P2 review finding: a stale
// infrastructureTruncated=true from the content read must not survive into
// limitations alongside infrastructureReadDegradedReason when the graph
// fallback ALSO fails. The two reasons assert mutually exclusive facts about
// the SAME read (repository_infrastructure_degrade.go,
// infrastructureTruncatedReason's doc comment): a failed read has no rows to
// bound, and a bounded read did not fail. Before this fix, the graph-read
// error branch appended infrastructureReadDegradedReason but never reset
// infrastructureTruncated, so a stale truncated=true from the content read
// survived into limitations alongside it -- "more rows may exist" attached to
// an EMPTY infrastructure panel.
func TestGetServiceContextReadModelResetsTruncatedOnGraphFallbackError(t *testing.T) {
	t.Parallel()

	rawEntities := make([]EntityContent, repositoryInfrastructureEntityLimit+1)
	for i := range rawEntities {
		rawEntities[i] = EntityContent{
			RepoID:       "repo-serverless-degrade",
			RelativePath: fmt.Sprintf("src/fn-%d.go", i),
			EntityType:   "Function",
			EntityName:   fmt.Sprintf("fn-%d", i),
		}
	}

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphUnavailable)
				}
				return nil, nil
			},
		},
		Content: nonFilteringInfrastructureContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{
					ID:   "repo-serverless-degrade",
					Name: "serverless-degrade",
					Path: "/repos/serverless-degrade",
				}},
				summary: repositoryReadModelSummary{
					Available:     true,
					WorkloadNames: []string{"serverless-degrade"},
				},
			},
			rawEntities: rawEntities,
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/serverless-degrade/context", nil)
	req.SetPathValue("service_name", "serverless-degrade")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	infrastructure, ok := resp["infrastructure"].([]any)
	if !ok || len(infrastructure) != 0 {
		t.Fatalf("infrastructure = %#v, want empty list (degraded, not fabricated rows)", resp["infrastructure"])
	}

	limitations, ok := resp["limitations"].([]any)
	if !ok {
		t.Fatalf("limitations missing or wrong type: %#v", resp["limitations"])
	}
	if !jsonStringSliceContains(limitations, infrastructureReadDegradedReason) {
		t.Fatalf("limitations = %#v, want to contain %q", limitations, infrastructureReadDegradedReason)
	}
	if jsonStringSliceContains(limitations, infrastructureTruncatedReason) {
		t.Fatalf("limitations = %#v, want NOT to contain %q alongside %q (degraded and truncated are mutually exclusive per read)",
			limitations, infrastructureTruncatedReason, infrastructureReadDegradedReason)
	}
}

// TestGetServiceContextReadModelDropsTruncatedOnEmptyGraphFallbackPanel covers
// the second path to the same wrong state, which the graph-error-branch reset
// above could not reach (#5764 round-7 P3). Here the graph fallback SUCCEEDS:
// it returns a full limit+1 window, so queryRepoInfrastructureFromGraph reports
// truncated=true, but isRepositoryInfrastructureType drops every row and the
// panel comes back empty. "infrastructure_truncated" then described a panel
// with nothing in it to bound. The read did not fail, so the error branch
// never runs -- only the post-attempt empty-panel guard catches this.
func TestGetServiceContextReadModelDropsTruncatedOnEmptyGraphFallbackPanel(t *testing.T) {
	t.Parallel()

	graphRows := make([]map[string]any, repositoryInfrastructureEntityLimit+1)
	for i := range graphRows {
		graphRows[i] = map[string]any{
			"type":      "Function",
			"name":      fmt.Sprintf("fn-%d", i),
			"file_path": fmt.Sprintf("src/fn-%d.go", i),
		}
	}

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return graphRows, nil
				}
				return nil, nil
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo-serverless-overreturn",
				Name: "serverless-overreturn",
				Path: "/repos/serverless-overreturn",
			}},
			summary: repositoryReadModelSummary{
				Available:     true,
				WorkloadNames: []string{"serverless-overreturn"},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/serverless-overreturn/context", nil)
	req.SetPathValue("service_name", "serverless-overreturn")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	infrastructure, ok := resp["infrastructure"].([]any)
	if !ok || len(infrastructure) != 0 {
		t.Fatalf("infrastructure = %#v, want empty list", resp["infrastructure"])
	}

	limitations, ok := resp["limitations"].([]any)
	if !ok {
		t.Fatalf("limitations missing or wrong type: %#v", resp["limitations"])
	}
	if jsonStringSliceContains(limitations, infrastructureTruncatedReason) {
		t.Fatalf("limitations = %#v, want NOT to contain %q on an EMPTY infrastructure panel", limitations, infrastructureTruncatedReason)
	}
}
