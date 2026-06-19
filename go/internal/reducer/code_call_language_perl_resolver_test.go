package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestCodeCallResolutionMethodPerlPackageImportBinding(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/bin/worker.pl"
	calleePath := "/repo/lib/App/Util.pm"
	decoyPath := "/repo/lib/App/Other.pm"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-perl",
			"imports_map": map[string][]string{
				"App::Util":  {calleePath},
				"App::Other": {decoyPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "bin/worker.pl",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "class_context": "Worker", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "App::Util", "lang": "perl"},
				},
				"function_calls": []any{
					map[string]any{"name": "App::Util::execute", "full_name": "App::Util::execute", "line_number": 5, "lang": "perl"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "lib/App/Util.pm",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "execute", "class_context": "Util", "line_number": 3, "end_line": 5, "uid": "uid:execute-util"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "lib/App/Other.pm",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "execute", "class_context": "Other", "line_number": 3, "end_line": 5, "uid": "uid:execute-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:execute-util"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:execute-decoy")
}

func TestCodeCallPerlAmbiguousPackageImportDoesNotResolve(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/bin/worker.pl"
	leftPath := "/repo/lib/App/Util.pm"
	rightPath := "/repo/vendor/App/Util.pm"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-perl",
			"imports_map": map[string][]string{
				"App::Util": {leftPath, rightPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "bin/worker.pl",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "App::Util", "lang": "perl"},
				},
				"function_calls": []any{
					map[string]any{"name": "App::Util::execute", "full_name": "App::Util::execute", "line_number": 5, "lang": "perl"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "lib/App/Util.pm",
			"parsed_file_data": map[string]any{
				"path": leftPath,
				"functions": []any{
					map[string]any{"name": "execute", "class_context": "Util", "line_number": 3, "end_line": 5, "uid": "uid:execute-left"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-perl",
			"relative_path": "vendor/App/Util.pm",
			"parsed_file_data": map[string]any{
				"path": rightPath,
				"functions": []any{
					map[string]any{"name": "execute", "class_context": "Util", "line_number": 3, "end_line": 5, "uid": "uid:execute-right"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:execute-left")
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:execute-right")
}

func TestCodeCallPerlParserOutputResolvesPackageImportBinding(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "bin", "worker.pl")
	calleePath := filepath.Join(repoRoot, "lib", "App", "Util.pm")
	writeReducerTestFile(t, callerPath, `package App::Worker;
use App::Util;

sub run {
  App::Util::execute();
}
`)
	writeReducerTestFile(t, calleePath, `package App::Util;

sub execute {
  return 1;
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	if len(importsMap["App::Util"]) == 0 {
		t.Fatalf("importsMap[App::Util] = %#v, want callee path", importsMap["App::Util"])
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignParserUID(t, callerPayload, "functions", "run", "uid:caller")
	assignParserUID(t, calleePayload, "functions", "execute", "uid:execute")

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-perl",
			"imports_map": importsMap,
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":          "repo-perl",
			"relative_path":    "bin/worker.pl",
			"parsed_file_data": callerPayload,
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":          "repo-perl",
			"relative_path":    "lib/App/Util.pm",
			"parsed_file_data": calleePayload,
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:execute"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q; rows=%#v", got, codeprovenance.MethodImportBinding, rows)
	}
}

func assignParserUID(t *testing.T, payload map[string]any, bucket string, name string, uid string) {
	t.Helper()
	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		if item["name"] == name {
			item["uid"] = uid
			return
		}
	}
	t.Fatalf("payload[%s] missing %q in %#v", bucket, name, items)
}
