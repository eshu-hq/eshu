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

func TestHandleRelationshipsSurfacesRelatedSymbolSourceMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				for _, fragment := range []string{
					"source_repo_id: repo.id",
					"target_repo_id: targetRepo.id",
					"source_file_path: f.relative_path",
					"target_file_path: targetFile.relative_path",
					"source_start_line: e.start_line",
					"target_start_line: target.start_line",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("relationship graph cypher missing %q:\n%s", fragment, cypher)
					}
				}
				return map[string]any{
					"id":         "function-center",
					"name":       "handlePayment",
					"labels":     []any{"Function"},
					"file_path":  "server/handlers/payment.ts",
					"repo_id":    "repository:payments",
					"repo_name":  "payments",
					"language":   "typescript",
					"start_line": int64(20),
					"end_line":   int64(35),
					"outgoing": []any{
						map[string]any{
							"direction":         "outgoing",
							"type":              "CALLS",
							"source_id":         "function-center",
							"source_name":       "handlePayment",
							"source_repo_id":    "repository:payments",
							"source_repo_name":  "payments",
							"source_file_path":  "server/handlers/payment.ts",
							"source_language":   "typescript",
							"source_type":       "Function",
							"source_start_line": int64(20),
							"source_end_line":   int64(35),
							"target_id":         "function-target",
							"target_name":       "chargeCard",
							"target_repo_id":    "repository:billing",
							"target_repo_name":  "billing",
							"target_file_path":  "src/billing/card.ts",
							"target_language":   "typescript",
							"target_type":       "Function",
							"target_start_line": int64(44),
							"target_end_line":   int64(58),
						},
					},
					"incoming": []any{
						map[string]any{
							"direction":         "incoming",
							"type":              "CALLS",
							"source_id":         "function-source",
							"source_name":       "validatePayment",
							"source_repo_id":    "repository:api",
							"source_repo_name":  "api",
							"source_file_path":  "src/api/validate.ts",
							"source_language":   "typescript",
							"source_type":       "Function",
							"source_start_line": int64(11),
							"source_end_line":   int64(19),
							"target_id":         "function-center",
							"target_name":       "handlePayment",
							"target_repo_id":    "repository:payments",
							"target_repo_name":  "payments",
							"target_file_path":  "server/handlers/payment.ts",
							"target_language":   "typescript",
							"target_type":       "Function",
							"target_start_line": int64(20),
							"target_end_line":   int64(35),
						},
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-center","relationship_type":"CALLS"}`),
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
	outgoing := relationshipRowsFromResponse(t, resp, "outgoing")
	assertRelationshipFields(t, outgoing[0], map[string]any{
		"source_repo_id":    "repository:payments",
		"source_file_path":  "server/handlers/payment.ts",
		"source_type":       "Function",
		"source_start_line": float64(20),
		"target_repo_id":    "repository:billing",
		"target_file_path":  "src/billing/card.ts",
		"target_type":       "Function",
		"target_start_line": float64(44),
	})
	incoming := relationshipRowsFromResponse(t, resp, "incoming")
	assertRelationshipFields(t, incoming[0], map[string]any{
		"source_repo_id":    "repository:api",
		"source_file_path":  "src/api/validate.ts",
		"source_type":       "Function",
		"source_start_line": float64(11),
		"target_repo_id":    "repository:payments",
		"target_file_path":  "server/handlers/payment.ts",
		"target_type":       "Function",
		"target_start_line": float64(20),
	})
}

func TestNornicDBOneHopRelationshipsCypherProjectsRelatedSymbolSourceMetadata(t *testing.T) {
	t.Parallel()

	// The core read carries the related symbol's own identity and line span
	// (function-safe without OPTIONAL MATCH); file path, repository, and file
	// language are restored by the separate enrichment reads and merged in Go.
	cypher, _ := nornicDBOneHopRelationshipsCypher("function-center", "outgoing", "CALLS", "Function", "uid")
	for _, fragment := range []string{
		"coalesce(target.id, target.uid) as target_entity_uid",
		"coalesce(target.id, target.uid) as target_id",
		"target.start_line as target_start_line",
		"head(labels(target)) as target_type",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("outgoing NornicDB core cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if strings.Contains(cypher, "OPTIONAL MATCH") {
		t.Fatalf("outgoing NornicDB core cypher must not contain OPTIONAL MATCH:\n%s", cypher)
	}

	// File and repository metadata are enriched by SEPARATE OPTIONAL-MATCH-free
	// reads so a File without a REPO_CONTAINS edge still yields its path/language.
	farFile := nornicDBRelationshipFarFileEnrichmentCypher("outgoing", "CALLS", "Function", "uid")
	for _, fragment := range []string{
		"-[:CALLS]->(enrichNode)<-[:CONTAINS]-(enrichFile:File)",
		"coalesce(enrichNode.id, enrichNode.uid) as entity_uid",
		"enrichFile.relative_path as file_path",
	} {
		if !strings.Contains(farFile, fragment) {
			t.Fatalf("outgoing far File enrichment cypher missing %q:\n%s", fragment, farFile)
		}
	}
	if strings.Contains(farFile, "REPO_CONTAINS") {
		t.Fatalf("far File enrichment cypher must not require a Repository edge:\n%s", farFile)
	}

	farRepo := nornicDBRelationshipFarRepoEnrichmentCypher("outgoing", "CALLS", "Function", "uid")
	if !strings.Contains(farRepo, "-[:CALLS]->(enrichNode)<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)") {
		t.Fatalf("outgoing far Repository enrichment cypher missing full path:\n%s", farRepo)
	}
	if !strings.Contains(farRepo, "enrichRepo.id as repo_id") {
		t.Fatalf("outgoing far Repository enrichment cypher missing repo_id:\n%s", farRepo)
	}

	for _, c := range []string{farFile, farRepo} {
		if strings.Contains(c, "OPTIONAL MATCH") {
			t.Fatalf("far enrichment cypher must not contain OPTIONAL MATCH:\n%s", c)
		}
	}

	farFileIncoming := nornicDBRelationshipFarFileEnrichmentCypher("incoming", "CALLS", "Function", "uid")
	if !strings.Contains(farFileIncoming, "<-[:CALLS]-(enrichNode)<-[:CONTAINS]-(enrichFile:File)") {
		t.Fatalf("incoming far File enrichment cypher missing reverse traversal:\n%s", farFileIncoming)
	}

	anchorFile := nornicDBRelationshipAnchorFileEnrichmentCypher("Function", "uid")
	if !strings.Contains(anchorFile, "(e:Function {uid: $entity_id})<-[:CONTAINS]-(enrichFile:File)") {
		t.Fatalf("anchor File enrichment cypher missing indexed anchor:\n%s", anchorFile)
	}
	if strings.Contains(anchorFile, "REPO_CONTAINS") {
		t.Fatalf("anchor File enrichment cypher must not require a Repository edge:\n%s", anchorFile)
	}
	anchorRepo := nornicDBRelationshipAnchorRepoEnrichmentCypher("Function", "uid")
	if !strings.Contains(anchorRepo, "(e:Function {uid: $entity_id})<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)") {
		t.Fatalf("anchor Repository enrichment cypher missing full path:\n%s", anchorRepo)
	}
}

func relationshipRowsFromResponse(t *testing.T, resp map[string]any, key string) []map[string]any {
	t.Helper()

	raw, ok := resp[key].([]any)
	if !ok {
		t.Fatalf("resp[%s] type = %T, want []any", key, resp[key])
	}
	if len(raw) != 1 {
		t.Fatalf("len(resp[%s]) = %d, want 1", key, len(raw))
	}
	row, ok := raw[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[%s][0] type = %T, want map[string]any", key, raw[0])
	}
	return []map[string]any{row}
}

func assertRelationshipFields(t *testing.T, row map[string]any, want map[string]any) {
	t.Helper()

	for key, wantValue := range want {
		if got := row[key]; !reflect.DeepEqual(got, wantValue) {
			t.Fatalf("relationship[%s] = %#v, want %#v", key, got, wantValue)
		}
	}
}
