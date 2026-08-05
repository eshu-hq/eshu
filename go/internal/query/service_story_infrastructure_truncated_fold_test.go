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

// TestGetServiceStoryInfrastructureTruncatedSetsResultLimitsTruncated is a
// round-11 review follow-up to #5764 (PR #5936, chatgpt-codex-connector
// finding 2): when a service's resolved repository has more than
// repositoryInfrastructureEntityLimit infrastructure rows,
// fetchWorkloadContextForOperation (entity_workload_context.go) appends
// infrastructureTruncatedReason to the workload context's "limitations", and
// buildServiceIdentity/the dossier whitelist loop
// (service_story_dossier.go's enrichServiceStoryDossierResponseWithContext)
// copy that reason into answer_metadata.partial_reasons -- but before this
// fix buildServiceResultLimitsWithContext computed result_limits.truncated
// from endpoint/upstream/dependent/consumer counts only, never looking at
// "limitations". BuildAnswerMetadata (answer_metadata.go) and
// serviceStoryAnswerData (answer_packet_routes.go) both read truncation from
// data["truncated"] (absent on the story response) or
// result_limits.truncated, neither of which included the infrastructure cap,
// so answer_metadata.truncated and the answer_packet's own "truncated" field
// stayed false even though the infrastructure evidence was clipped. This
// mirrors the already-fixed repository-story sibling
// (TestGetRepositoryStoryInfrastructureTruncatedSetsTopLevelTruncated,
// repository_story_infrastructure_truncated_fold_test.go): the fake graph
// reader returns repositoryInfrastructureEntityLimit+1 infrastructure rows so
// this test isolates the infrastructure-only truncation source, and drives
// the real handler through its mounted route rather than a helper.
func TestGetServiceStoryInfrastructureTruncatedSetsResultLimitsTruncated(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":        "workload:svc-story-infra-trunc",
					"name":      "svc-story-infra-trunc",
					"kind":      "service",
					"repo_id":   "repo-svc-story-infra-trunc",
					"repo_name": "svc-story-infra-trunc",
				},
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.name = $service_name"):
					// Candidate resolution: exactly one match so
					// resolveServiceWorkloadCandidate proceeds to the exact
					// w.id lookup above instead of reporting ambiguity.
					return []map[string]any{{
						"id":      "workload:svc-story-infra-trunc",
						"name":    "svc-story-infra-trunc",
						"kind":    "service",
						"repo_id": "repo-svc-story-infra-trunc",
					}}, nil
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-svc-story-infra-trunc", "repo_name": "svc-story-infra-trunc"}}, nil
				case strings.Contains(cypher, infrastructureGraphReadCypherFragment):
					limit := IntVal(params, "limit")
					rows := make([]map[string]any, limit)
					for i := range rows {
						rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
					}
					return rows, nil
				default:
					return nil, nil
				}
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/svc-story-infra-trunc/story", nil)
	req.SetPathValue("service_name", "svc-story-infra-trunc")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// The infrastructure cap must still be visible under partial_reasons
	// (already working before this fix, kept here as the control).
	answerMetadata, ok := body["answer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_metadata] missing or wrong type: %#v", body["answer_metadata"])
	}
	partialReasons, ok := answerMetadata["partial_reasons"].([]any)
	if !ok || !jsonStringSliceContains(partialReasons, infrastructureTruncatedReason) {
		t.Fatalf("answer_metadata[partial_reasons] = %#v, want to contain %q", answerMetadata["partial_reasons"], infrastructureTruncatedReason)
	}

	// result_limits.truncated must fold in the infrastructure cap.
	resultLimits, ok := body["result_limits"].(map[string]any)
	if !ok {
		t.Fatalf("body[result_limits] missing or wrong type: %#v", body["result_limits"])
	}
	if truncated, _ := resultLimits["truncated"].(bool); !truncated {
		t.Fatalf("result_limits.truncated = %#v, want true when only infrastructure_truncated fired", resultLimits["truncated"])
	}

	// answer_metadata.truncated derives from result_limits.truncated
	// (answerMetadataCoverage falls back to result_limits when no top-level
	// "truncated"/"coverage" key exists on the story response) and must
	// reflect the same fold.
	if truncated, _ := answerMetadata["truncated"].(bool); !truncated {
		t.Fatalf("answer_metadata.truncated = %#v, want true when only infrastructure_truncated fired", answerMetadata["truncated"])
	}

	// The answer_packet -- the actual response payload get_service_story
	// callers read -- must carry the same signal, not just its unwrapped
	// data companion.
	answerPacket, ok := body["answer_packet"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_packet] missing or wrong type: %#v", body["answer_packet"])
	}
	if truncated, _ := answerPacket["truncated"].(bool); !truncated {
		t.Fatalf("answer_packet.truncated = %#v, want true when only infrastructure_truncated fired", answerPacket["truncated"])
	}
}
