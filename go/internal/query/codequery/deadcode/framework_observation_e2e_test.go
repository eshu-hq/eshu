// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestHandleDeadCodeReportsProducerObservedFrameworks proves end to end that
// the no-root-model notice fires from production-shaped inputs: the framework
// values on the graph rows come from real parser output, not hand-written
// row injection. A go gin handler (observed, unmodeled) fires the notice;
// a php symfony method (observed, modeled) stays silent.
func TestHandleDeadCodeReportsProducerObservedFrameworks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	goPath := filepath.Join(repoRoot, "routes.go")
	if err := os.WriteFile(goPath, []byte(`package test

import gin "github.com/gin-gonic/gin"

func wire() {
	router := gin.New()
	router.GET("/health", Health)
}

func Health() {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(go) error = %v, want nil", err)
	}
	phpPath := filepath.Join(repoRoot, "ReportController.php")
	if err := os.WriteFile(phpPath, []byte(`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController {
    #[Route('/reports/{id}', methods: ['GET'])]
    public function show(): string {
        return 'show';
    }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(php) error = %v, want nil", err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	goPayload, err := engine.ParsePath(repoRoot, goPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(go) error = %v, want nil", err)
	}
	phpPayload, err := engine.ParsePath(repoRoot, phpPath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(php) error = %v, want nil", err)
	}

	rows := []map[string]any{}
	for _, parsed := range []struct {
		payload  map[string]any
		language string
		path     string
	}{
		{goPayload, "go", "routes.go"},
		{phpPayload, "php", "ReportController.php"},
	} {
		functions, _ := parsed.payload["functions"].([]map[string]any)
		if len(functions) == 0 {
			t.Fatalf("ParsePath(%s) functions = empty, want production facts", parsed.path)
		}
		for _, function := range functions {
			framework, _ := function["framework"].(string)
			if framework == "" {
				continue
			}
			name, _ := function["name"].(string)
			rows = append(rows, map[string]any{
				"entity_id": parsed.language + ":" + name,
				"name":      name, "labels": []any{"Function"},
				"file_path": parsed.path, "repo_id": "repo-1", "repo_name": "framework-app",
				"language": parsed.language, "framework": framework,
			})
		}
	}
	if len(rows) == 0 {
		t.Fatal("no parsed function carried framework: producers emitted nothing to observe")
	}

	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: graph.FakeGraphReader{
			RunFn: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":50}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	analysis := data["analysis"].(map[string]any)

	unmodeled, ok := analysis["frameworks_without_root_model"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[frameworks_without_root_model] type = %T, want map[string]any", analysis["frameworks_without_root_model"])
	}
	goFrameworks, ok := unmodeled["go"].([]any)
	if !ok || len(goFrameworks) != 1 || goFrameworks[0] != "gin" {
		t.Fatalf("frameworks_without_root_model[go] = %#v, want [gin]", unmodeled["go"])
	}
	if _, present := unmodeled["php"]; present {
		t.Fatalf("frameworks_without_root_model[php] = %#v, want absent (symfony is modeled)", unmodeled["php"])
	}
	notes, _ := analysis["notes"].([]any)
	found := false
	for _, note := range notes {
		if text, _ := note.(string); strings.Contains(text, "go(gin)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("analysis notes = %#v, want an entry naming go(gin)", notes)
	}
}
