package query

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
)

type nodeTypeScriptDeadCodeMatrix struct {
	Cases []nodeTypeScriptDeadCodeCase `json:"cases"`
}

type nodeTypeScriptDeadCodeCase struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	EntityType string   `json:"entity_type"`
	Language   string   `json:"language"`
	Path       string   `json:"path"`
	SourceFile string   `json:"source_file"`
	RootKinds  []string `json:"root_kinds"`
	Expected   string   `json:"expected"`
}

func TestHandleDeadCodeNodeTypeScriptFixtureMatrix(t *testing.T) {
	t.Parallel()

	matrix := loadNodeTypeScriptDeadCodeMatrix(t)
	rows := make([]map[string]any, 0, len(matrix.Cases))
	entities := make(map[string]EntityContent, len(matrix.Cases))
	for _, item := range matrix.Cases {
		rows = append(rows, nodeTypeScriptDeadCodeRow(item))
		entity := EntityContent{
			EntityID:     item.ID,
			RepoID:       "repo-1",
			RelativePath: item.Path,
			EntityType:   item.EntityType,
			EntityName:   item.Name,
			Language:     item.Language,
			SourceCache:  readNodeTypeScriptFixtureSource(t, item.SourceFile),
		}
		if len(item.RootKinds) > 0 {
			entity.Metadata = map[string]any{"dead_code_root_kinds": item.RootKinds}
		}
		entities[item.ID] = entity
	}

	var scanLimits []int
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:Function") {
					return nil, nil
				}
				if !strings.Contains(cypher, "SKIP $skip") || !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded candidate scan", cypher)
				}
				limit, ok := params["limit"].(int)
				if !ok {
					t.Fatalf("params[limit] type = %T, want int", params["limit"])
				}
				scanLimits = append(scanLimits, limit)
				return rows, nil
			},
		},
		Content: fakeDeadCodeContentStore{entities: entities},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":25}`),
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
	if got, want := resp["candidate_scan_truncated"], false; got != want {
		t.Fatalf("resp[candidate_scan_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["display_truncated"], false; got != want {
		t.Fatalf("resp[display_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_rows"], float64(len(matrix.Cases)); got != want {
		t.Fatalf("resp[candidate_scan_rows] = %#v, want %#v", got, want)
	}
	if got, want := scanLimits, []int{deadCodeCandidateQueryLimit(25)}; !equalIntSlices(got, want) {
		t.Fatalf("scan limits = %#v, want %#v", got, want)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	resultIDs := make(map[string]struct{}, len(results))
	for _, result := range results {
		item, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want map[string]any", result)
		}
		resultIDs[StringVal(item, "entity_id")] = struct{}{}
	}

	for _, item := range matrix.Cases {
		_, returned := resultIDs[item.ID]
		switch item.Expected {
		case "dead":
			if !returned {
				t.Fatalf("%s expected dead-code result, got none", item.ID)
			}
		case "live", "excluded":
			if returned {
				t.Fatalf("%s expected %s, got dead-code result", item.ID, item.Expected)
			}
		default:
			t.Fatalf("%s has unknown expected value %q", item.ID, item.Expected)
		}
	}
}

func loadNodeTypeScriptDeadCodeMatrix(t *testing.T) nodeTypeScriptDeadCodeMatrix {
	t.Helper()

	path := filepath.Join(nodeTypeScriptDeadCodeFixtureRoot(), "expected.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	var matrix nodeTypeScriptDeadCodeMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", path, err)
	}
	if len(matrix.Cases) == 0 {
		t.Fatalf("%s has no cases", path)
	}
	return matrix
}

func readNodeTypeScriptFixtureSource(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(nodeTypeScriptDeadCodeFixtureRoot(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(data)
}

func nodeTypeScriptDeadCodeFixtureRoot() string {
	return filepath.Join("..", "..", "..", "tests", "fixtures", "dead-code", "node-typescript")
}

func nodeTypeScriptDeadCodeRow(item nodeTypeScriptDeadCodeCase) map[string]any {
	return map[string]any{
		"entity_id":  item.ID,
		"name":       item.Name,
		"labels":     []any{item.EntityType},
		"file_path":  item.Path,
		"repo_id":    "repo-1",
		"repo_name":  "node-typescript-fixture",
		"language":   item.Language,
		"start_line": int64(1),
		"end_line":   int64(3),
	}
}
