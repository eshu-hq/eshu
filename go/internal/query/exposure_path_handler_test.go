// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// exposureSourceContentStore is a content store that returns one source entity by
// id and by exact name, carrying the dead_code_root_kinds the classifier reads.
type exposureSourceContentStore struct {
	fakePortContentStore
	entity EntityContent
}

func (s exposureSourceContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	if entityID != s.entity.EntityID {
		return nil, nil
	}
	cloned := s.entity
	return &cloned, nil
}

func (s exposureSourceContentStore) SearchEntitiesByName(_ context.Context, _ string, _ string, name string, _ int) ([]EntityContent, error) {
	if name != s.entity.EntityName {
		return nil, nil
	}
	return []EntityContent{s.entity}, nil
}

func postExposurePath(t *testing.T, h *ImpactHandler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-exposure-path", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
		}
	}
	return rec, decoded
}

func httpHandlerSourceEntity() EntityContent {
	return EntityContent{
		EntityID:   "fn:handler",
		RepoID:     "repo-1",
		EntityName: "HandleRequest",
		EntityType: "function",
		Metadata: map[string]any{
			"dead_code_root_kinds": []any{"go.net_http_handler_signature"},
		},
	}
}

// TestTraceExposurePathRendersReachableSink proves the handler renders a reachable
// cloud-sink path with the conservative truth-state vocabulary, a justified
// severity, and the derived truth label. The source is an HTTP handler; because a
// Function->Endpoint bridge does not exist to prove internet reachability, the
// source is honestly ranked network_reachable, capping the IAM-escalation
// severity at high rather than over-claiming critical.
func TestTraceExposurePathRendersReachableSink(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Content: exposureSourceContentStore{entity: httpHandlerSourceEntity()},
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got := params["source_entity_id"]; got != "fn:handler" {
					t.Fatalf("source_entity_id param = %#v", got)
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{"id": "fn:handler", "name": "HandleRequest", "labels": []any{"Function"}},
							map[string]any{"id": "cr:role", "name": "deploy-role", "labels": []any{"CloudResource"}},
						},
						"sink_rel":    "CAN_ESCALATE_TO",
						"sink_node":   map[string]any{"id": "cr:admin", "name": "admin", "labels": []any{"CloudResource"}},
						"sink_labels": []any{"CloudResource"},
						"depth":       1,
					},
				}, nil
			},
		},
	}

	rec, body := postExposurePath(t, handler, `{"source_entity_id":"fn:handler"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := body["source_kind"]; got != "http_handler" {
		t.Fatalf("source_kind = %#v, want http_handler", got)
	}
	if got := body["exposure_rank"]; got != "network_reachable" {
		t.Fatalf("exposure_rank = %#v, want network_reachable (internet reachability unproven)", got)
	}
	if got := body["truth_label"]; got != "derived" {
		t.Fatalf("truth_label = %#v, want derived", got)
	}
	if got := body["state"]; got != "exact" {
		t.Fatalf("state = %#v, want exact", got)
	}
	paths, ok := body["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v, want 1", body["paths"])
	}
	path := paths[0].(map[string]any)
	if got := path["severity"]; got != "high" {
		t.Fatalf("severity = %#v, want high (network_reachable caps IAM critical at high)", got)
	}
	sink := path["sink"].(map[string]any)
	if got := sink["kind"]; got != "iam_privileged_action" {
		t.Fatalf("sink kind = %#v, want iam_privileged_action", got)
	}
	if path["reason"] == "" {
		t.Fatal("path must carry a severity reason")
	}
}

func TestTraceExposurePathRendersShellExecSink(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Content: exposureSourceContentStore{entity: httpHandlerSourceEntity()},
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				sinkRels, _ := params["sink_rels"].([]string)
				found := false
				for _, rel := range sinkRels {
					if rel == "EXECUTES_SHELL" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("sink_rels = %#v, missing EXECUTES_SHELL", sinkRels)
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{"id": "fn:handler", "name": "HandleRequest", "labels": []any{"Function"}},
						},
						"sink_rel":    "EXECUTES_SHELL",
						"sink_node":   map[string]any{"id": "shell-command:abc123", "name": "command execution", "labels": []any{"ShellCommand"}},
						"sink_labels": []any{"ShellCommand"},
						"depth":       0,
					},
				}, nil
			},
		},
	}

	rec, body := postExposurePath(t, handler, `{"source_entity_id":"fn:handler"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	paths, ok := body["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v, want 1", body["paths"])
	}
	path := paths[0].(map[string]any)
	sink := path["sink"].(map[string]any)
	if got := sink["kind"]; got != "shell_exec" {
		t.Fatalf("sink kind = %#v, want shell_exec", got)
	}
	if got := path["severity"]; got != "high" {
		t.Fatalf("severity = %#v, want high", got)
	}
}

// TestTraceExposurePathUnresolvedWhenNoSink proves that when the graph yields no
// reachable sink (production reality until the bridge edges materialize), the
// handler returns an unresolved finding with an honest reason and zero paths —
// never a fabricated path.
func TestTraceExposurePathUnresolvedWhenNoSink(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Content: exposureSourceContentStore{entity: httpHandlerSourceEntity()},
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
	}

	rec, body := postExposurePath(t, handler, `{"source":"HandleRequest","repo_id":"repo-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body["state"]; got != "unresolved" {
		t.Fatalf("state = %#v, want unresolved", got)
	}
	if paths, ok := body["paths"].([]any); ok && len(paths) != 0 {
		t.Fatalf("paths = %d, want 0", len(paths))
	}
	coverage := body["coverage"].(map[string]any)
	if coverage["unresolved_reason"] == "" {
		t.Fatal("unresolved finding must record a reason")
	}
}

// TestTraceExposurePathRejectsNonSource proves a function that is not a taint
// source (e.g. main) yields an unresolved finding explaining it is not a source.
func TestTraceExposurePathRejectsNonSource(t *testing.T) {
	t.Parallel()

	entity := httpHandlerSourceEntity()
	entity.Metadata = map[string]any{"dead_code_root_kinds": []any{"go.main"}}
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Content: exposureSourceContentStore{entity: entity},
		Neo4j:   fakeGraphReader{},
	}

	rec, body := postExposurePath(t, handler, `{"source_entity_id":"fn:handler"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body["state"]; got != "unresolved" {
		t.Fatalf("state = %#v, want unresolved", got)
	}
	if got := body["source_kind"]; got != "" {
		t.Fatalf("source_kind = %#v, want empty for a non-source", got)
	}
}

// TestTraceExposurePathRequiresAuthoritativeProfile proves the capability gate
// rejects the lightweight profile.
func TestTraceExposurePathRequiresAuthoritativeProfile(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalLightweight}
	rec, _ := postExposurePath(t, handler, `{"source_entity_id":"fn:handler"}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

// TestTraceExposurePathRequiresSource proves a missing source is a 400.
func TestTraceExposurePathRequiresSource(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalAuthoritative}
	rec, _ := postExposurePath(t, handler, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
