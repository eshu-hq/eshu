package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesParsedRubyReceiverlessHelpers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app", "controllers", "api_controller.rb")
	writeReducerTestFile(
		t,
		filePath,
		`class Admin::ApiController
  def create
    api_key.scopes = build_scopes
    log_api_key(api_key, changes: api_key.saved_changes)
  end

  private

  def build_scopes
    build_params(params)
  end

  def build_params(params)
    params
  end

  def log_api_key(*args)
    StaffActionLogger.new.log_api_key(*args)
  end
end
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	parsed, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}
	reducerTestAssignEntityUIDs(parsed)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-ruby"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":           "repo-ruby",
				"relative_path":     "app/controllers/api_controller.rb",
				"parsed_file_data":  parsed,
				"content_entity_id": "repo-ruby:app/controllers/api_controller.rb",
			},
		},
	})

	wantEdges := map[string]string{
		"create->build_scopes":       "",
		"create->log_api_key":        "",
		"build_scopes->build_params": "",
	}
	for _, row := range rows {
		caller := reducerTestEntityName(parsed, row["caller_entity_id"])
		callee := reducerTestEntityName(parsed, row["callee_entity_id"])
		key := caller + "->" + callee
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = key
		}
	}
	for key, got := range wantEdges {
		if got == "" {
			t.Fatalf("missing Ruby CALLS edge %s in rows=%#v", key, rows)
		}
	}
}

func reducerTestAssignEntityUIDs(parsed map[string]any) {
	for _, bucket := range []string{"functions", "classes", "modules"} {
		for _, item := range mapSlice(parsed[bucket]) {
			if anyToString(item["uid"]) != "" {
				continue
			}
			item["uid"] = "content-entity:" + bucket + ":" + anyToString(item["name"])
		}
	}
}

func reducerTestEntityName(parsed map[string]any, entityID any) string {
	id, _ := entityID.(string)
	for _, bucket := range []string{"functions", "classes", "modules"} {
		for _, item := range mapSlice(parsed[bucket]) {
			if item["uid"] == id {
				return anyToString(item["name"])
			}
		}
	}
	return id
}
