// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type nornicDBRelationshipContentStore struct {
	fakePortContentStore
	entities map[string]EntityContent
}

func (s nornicDBRelationshipContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	entity, ok := s.entities[entityID]
	if !ok {
		return nil, nil
	}
	copied := entity
	return &copied, nil
}

func TestHandleRelationshipsUsesNornicDBRowQueriesForDirectCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Content: nornicDBRelationshipContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "payments"}},
			},
			entities: map[string]EntityContent{
				"function-1": {
					EntityID:     "function-1",
					RepoID:       "repo-1",
					RelativePath: "src/payments.go",
					EntityType:   "Function",
					EntityName:   "handlePayment",
					StartLine:    10,
				},
			},
		},
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "collect(DISTINCT {") {
					t.Fatalf("cypher = %q, must not use map-collect projection on NornicDB", cypher)
				}
				switch {
				case strings.Contains(cypher, "<-[:CONTAINS]-(f:File)"):
					if got, want := params["name"], "handlePayment"; got != want {
						t.Fatalf("params[name] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"id":         "function-1",
						"name":       "handlePayment",
						"labels":     []any{"Function"},
						"file_path":  "src/payments.go",
						"repo_id":    "repo.id",
						"repo_name":  "repo.name",
						"language":   "go",
						"start_line": int64(10),
						"end_line":   int64(20),
					}}, nil
				case strings.Contains(cypher, "-[rel:CALLS]->(target)"):
					if !strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})") {
						t.Fatalf("cypher = %q, want indexed uid entity lookup", cypher)
					}
					if strings.Contains(cypher, graphEntityIDPredicate("e", "$entity_id")) {
						t.Fatalf("cypher = %q, must not use broad entity-id OR predicate on NornicDB", cypher)
					}
					return []map[string]any{{
						"direction":   "outgoing",
						"type":        "CALLS",
						"call_kind":   "row.call_kind",
						"reason":      "rel.reason",
						"target_name": "chargeCard",
						"target_id":   "function-2",
					}}, nil
				case strings.Contains(cypher, "MATCH (source)-[rel]->"):
					t.Fatalf("cypher = %q, must not fetch incoming relationships for outgoing-only request", cypher)
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"handlePayment","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("resp[outgoing] = %#v, want one relationship", resp["outgoing"])
	}
	relationship, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := relationship["target_name"], "chargeCard"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if _, ok := relationship["call_kind"]; ok {
		t.Fatalf("relationship[call_kind] = %#v, want absent placeholder property", relationship["call_kind"])
	}
	if _, ok := relationship["reason"]; ok {
		t.Fatalf("relationship[reason] = %#v, want absent placeholder property", relationship["reason"])
	}
	if got, want := resp["repo_id"], "repo-1"; got != want {
		t.Fatalf("resp[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := resp["repo_name"], "payments"; got != want {
		t.Fatalf("resp[repo_name] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipsUsesIndexedFallbackForNornicDBDirectCalls(t *testing.T) {
	t.Parallel()

	var metadataLookups []string
	var relationshipLookups []string
	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "<-[:CONTAINS]-(f:File)"):
					if strings.Contains(cypher, graphEntityIDPredicate("e", "$entity_id")) {
						t.Fatalf("cypher = %q, must not use broad metadata entity-id OR predicate on NornicDB", cypher)
					}
					switch {
					case strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})"):
						metadataLookups = append(metadataLookups, "uid")
						return []map[string]any{}, nil
					case strings.Contains(cypher, "MATCH (e:Function {id: $entity_id})"):
						metadataLookups = append(metadataLookups, "id")
					default:
						t.Fatalf("cypher = %q, want uid/id metadata lookup", cypher)
					}
					return []map[string]any{{
						"id":         "content-entity:handleRelationships",
						"name":       "handleRelationships",
						"labels":     []any{"Function"},
						"file_path":  "go/internal/query/code_relationships.go",
						"repo_id":    "repo-1",
						"repo_name":  "eshu",
						"language":   "go",
						"start_line": int64(22),
						"end_line":   int64(168),
					}}, nil
				case strings.Contains(cypher, "-[rel:CALLS]->(target)"):
					switch {
					case strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})"):
						relationshipLookups = append(relationshipLookups, "uid")
						return []map[string]any{}, nil
					case strings.Contains(cypher, "MATCH (e:Function {id: $entity_id})"):
						relationshipLookups = append(relationshipLookups, "id")
					default:
						t.Fatalf("cypher = %q, want indexed uid/id relationship lookup", cypher)
					}
					if got, want := params["entity_id"], "content-entity:handleRelationships"; got != want {
						t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"direction":   "outgoing",
						"type":        "CALLS",
						"target_name": "filterRelationshipResponse",
						"target_id":   "content-entity:filterRelationshipResponse",
					}}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
		Content: nornicDBRelationshipContentStore{
			entities: map[string]EntityContent{
				"content-entity:handleRelationships": {
					EntityID:     "content-entity:handleRelationships",
					RepoID:       "repo-1",
					RelativePath: "go/internal/query/code_relationships.go",
					EntityType:   "Function",
					EntityName:   "handleRelationships",
					StartLine:    22,
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"content-entity:handleRelationships","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("resp[outgoing] = %#v, want one relationship", resp["outgoing"])
	}
	if want := []string{"uid", "id"}; !reflect.DeepEqual(metadataLookups, want) {
		t.Fatalf("metadataLookups = %#v, want %#v", metadataLookups, want)
	}
	if want := []string{"uid", "id"}; !reflect.DeepEqual(relationshipLookups, want) {
		t.Fatalf("relationshipLookups = %#v, want %#v", relationshipLookups, want)
	}
}

func TestHandleRelationshipsMissingContentUsesLabeledIdentityProbes(t *testing.T) {
	t.Parallel()

	var probes []string
	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Content:      nornicDBRelationshipContentStore{entities: map[string]EntityContent{}},
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (e {uid: $entity_id})") ||
					strings.Contains(cypher, "MATCH (e {id: $entity_id})") {
					t.Fatalf("missing content entity used an unlabeled identity scan:\n%s", cypher)
				}
				if !strings.Contains(cypher, "MATCH (e:Function") || !strings.Contains(cypher, "\nUNION\n") {
					t.Fatalf("missing content entity did not use the fixed labeled identity probe:\n%s", cypher)
				}
				switch {
				case strings.Contains(cypher, "{uid: $entity_id}"):
					probes = append(probes, "uid")
				case strings.Contains(cypher, "{id: $entity_id}"):
					probes = append(probes, "id")
				default:
					t.Fatalf("identity probe omitted uid/id property:\n%s", cypher)
				}
				return []map[string]any{}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"content-entity:missing","max_depth":1}`),
	)
	rec := httptest.NewRecorder()

	handler.handleRelationships(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if want := []string{"uid", "id"}; !reflect.DeepEqual(probes, want) {
		t.Fatalf("identity probes = %#v, want %#v", probes, want)
	}
}

func TestHandleRelationshipsHydratesNornicDBPlaceholderRepoIdentityFromContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "<-[:CONTAINS]-(f:File)"):
					if !strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})") {
						t.Fatalf("cypher = %q, want indexed uid metadata lookup", cypher)
					}
					if strings.Contains(cypher, graphEntityIDPredicate("e", "$entity_id")) {
						t.Fatalf("cypher = %q, must not use broad metadata entity-id OR predicate on NornicDB", cypher)
					}
					return []map[string]any{{
						"id":         "content-entity:handleRelationships",
						"name":       "handleRelationships",
						"labels":     []any{"Function"},
						"file_path":  "go/internal/query/code_relationships.go",
						"repo_id":    "repo.id",
						"repo_name":  "repo.name",
						"language":   "go",
						"start_line": int64(22),
						"end_line":   int64(168),
					}}, nil
				case strings.Contains(cypher, "-[rel:CALLS]->(target)"):
					if !strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})") {
						t.Fatalf("cypher = %q, want indexed uid relationship lookup", cypher)
					}
					if got, want := params["entity_id"], "content-entity:handleRelationships"; got != want {
						t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"direction":   "outgoing",
						"type":        "CALLS",
						"target_name": "filterRelationshipResponse",
						"target_id":   "content-entity:filterRelationshipResponse",
					}}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
		Content: nornicDBRelationshipContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{
					{ID: "repo-1", Name: "eshu"},
				},
			},
			entities: map[string]EntityContent{
				"content-entity:handleRelationships": {
					EntityID:     "content-entity:handleRelationships",
					RepoID:       "repo-1",
					RelativePath: "go/internal/query/code_relationships.go",
					EntityType:   "Function",
					EntityName:   "handleRelationships",
					StartLine:    22,
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"content-entity:handleRelationships","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["repo_id"], "repo-1"; got != want {
		t.Fatalf("resp[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := resp["repo_name"], "eshu"; got != want {
		t.Fatalf("resp[repo_name] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipsUsesNornicDBBFSForTransitiveCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "CALLS*") || strings.Contains(cypher, "length(path)") {
					t.Fatalf("cypher = %q, must not depend on NornicDB variable-path length", cypher)
				}
				switch {
				case strings.Contains(cypher, "MATCH (e)<-[:CONTAINS]-(f:File)"):
					return []map[string]any{{
						"id":         "function-1",
						"name":       "handlePayment",
						"labels":     []any{"Function"},
						"file_path":  "src/payments.go",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "go",
						"start_line": int64(10),
						"end_line":   int64(20),
					}}, nil
				case strings.Contains(cypher, "MATCH (source)-[:CALLS]->(target)"):
					switch params["entity_id"] {
					case "function-1":
						return []map[string]any{{
							"source_id":   "function-1",
							"source_name": "handlePayment",
							"target_id":   "function-2",
							"target_name": "chargeCard",
						}}, nil
					case "function-2":
						return []map[string]any{{
							"source_id":   "function-2",
							"source_name": "chargeCard",
							"target_id":   "function-3",
							"target_name": "postLedger",
						}}, nil
					default:
						return []map[string]any{}, nil
					}
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"handlePayment","direction":"outgoing","relationship_type":"CALLS","transitive":true,"max_depth":2}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok || len(outgoing) != 2 {
		t.Fatalf("resp[outgoing] = %#v, want two transitive relationships", resp["outgoing"])
	}
	second, ok := outgoing[1].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][1] type = %T, want map[string]any", outgoing[1])
	}
	if got, want := second["target_name"], "postLedger"; got != want {
		t.Fatalf("second[target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["depth"], float64(2); got != want {
		t.Fatalf("second[depth] = %#v, want %#v", got, want)
	}
}
