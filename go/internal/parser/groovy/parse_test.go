package groovy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseBuildsGroovyPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Jenkinsfile")
	source := `@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := Parse(path, true, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if got["path"] != path || got["lang"] != "groovy" || got["is_dependency"] != true {
		t.Fatalf("payload identity = %#v, want path/lang/dependency", got)
	}
	assertEmptyNamedBucket(t, got, "functions")
	assertEmptyNamedBucket(t, got, "classes")
	assertEmptyNamedBucket(t, got, "imports")
	assertEmptyNamedBucket(t, got, "function_calls")
	assertEmptyNamedBucket(t, got, "variables")
	assertEmptyNamedBucket(t, got, "modules")
	assertEmptyNamedBucket(t, got, "module_inclusions")
	assertStringSliceContains(t, got["shared_libraries"].([]string), "pipelines")
	assertStringSliceContains(t, got["pipeline_calls"].([]string), "pipelineDeploy")
	assertStringSliceContains(t, got["entry_points"].([]string), "deploy.sh")
	if got["source"] != source {
		t.Fatalf("source = %#v, want original source", got["source"])
	}
}

func TestPreScanReturnsSortedUniqueMetadataNames(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Jenkinsfile")
	source := `pipelineDeploy(entry_point: 'deploy.sh')
@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := PreScan(path)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}

	want := []string{"deploy.sh", "pipelineDeploy", "pipelines"}
	if len(got) != len(want) {
		t.Fatalf("PreScan() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, item := range want {
		if got[i] != item {
			t.Fatalf("PreScan()[%d] = %q, want %q in %#v", i, got[i], item, got)
		}
	}
}

func assertEmptyNamedBucket(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	if len(items) != 0 {
		t.Fatalf("%s len = %d, want 0: %#v", key, len(items), items)
	}
}
