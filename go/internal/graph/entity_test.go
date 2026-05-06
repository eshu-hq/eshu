package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildEntityMergeStatementUIDIdentity(t *testing.T) {
	t.Parallel()

	stmt, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Function",
		FilePath:   "/repo/src/main.go",
		Name:       "main",
		LineNumber: 10,
		UID:        "func:main:main.go:10",
		Extra: map[string]any{
			"lang":   "go",
			"source": "func main() {}",
		},
	})
	if err != nil {
		t.Fatalf("BuildEntityMergeStatement() error = %v, want nil", err)
	}

	if !strings.Contains(stmt.Cypher, "MERGE (n:Function {uid: $uid})") {
		t.Fatalf("Cypher missing UID identity MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (f)-[:CONTAINS]->(n)") {
		t.Fatalf("Cypher missing CONTAINS edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["uid"] != "func:main:main.go:10" {
		t.Fatalf("uid = %v", stmt.Parameters["uid"])
	}
	if stmt.Parameters["file_path"] != "/repo/src/main.go" {
		t.Fatalf("file_path = %v", stmt.Parameters["file_path"])
	}
	if stmt.Parameters["lang"] != "go" {
		t.Fatalf("lang = %v", stmt.Parameters["lang"])
	}
}

func TestBuildEntityMergeStatementNameIdentity(t *testing.T) {
	t.Parallel()

	stmt, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Class",
		FilePath:   "/repo/src/app.py",
		Name:       "MyClass",
		LineNumber: 5,
	})
	if err != nil {
		t.Fatalf("BuildEntityMergeStatement() error = %v, want nil", err)
	}

	if !strings.Contains(stmt.Cypher, "name: $name, path: $file_path, line_number: $line_number") {
		t.Fatalf("Cypher missing name identity MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (n:Class") {
		t.Fatalf("Cypher missing Class label: %s", stmt.Cypher)
	}
}

func TestBuildEntityMergeStatementExtraProperties(t *testing.T) {
	t.Parallel()

	stmt, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Variable",
		FilePath:   "/repo/src/config.py",
		Name:       "MAX_RETRIES",
		LineNumber: 1,
		Extra: map[string]any{
			"lang":      "python",
			"docstring": "Maximum retry count",
			"value":     "3",
		},
	})
	if err != nil {
		t.Fatalf("BuildEntityMergeStatement() error = %v, want nil", err)
	}

	// Extra properties should be in the SET clause.
	for _, key := range []string{"docstring", "lang", "value"} {
		if !strings.Contains(stmt.Cypher, "n.`"+key+"`") {
			t.Errorf("SET clause missing property %q: %s", key, stmt.Cypher)
		}
		if _, ok := stmt.Parameters[key]; !ok {
			t.Errorf("Parameters missing key %q", key)
		}
	}
}

func TestBuildEntityMergeStatementRejectsInvalidLabel(t *testing.T) {
	t.Parallel()

	_, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Bad Label!",
		FilePath:   "/repo/test.py",
		Name:       "foo",
		LineNumber: 1,
	})
	if err == nil {
		t.Fatal("error = nil, want non-nil for invalid label")
	}
	if !strings.Contains(err.Error(), "invalid Cypher label") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildEntityMergeStatementRejectsInvalidPropertyKey(t *testing.T) {
	t.Parallel()

	_, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Function",
		FilePath:   "/repo/test.py",
		Name:       "foo",
		LineNumber: 1,
		Extra: map[string]any{
			"valid_key":  "ok",
			"bad key!!!": "not ok",
		},
	})
	if err == nil {
		t.Fatal("error = nil, want non-nil for invalid property key")
	}
	if !strings.Contains(err.Error(), "invalid Cypher property key") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildEntityMergeStatementRejectsEmptyFilePath(t *testing.T) {
	t.Parallel()

	_, err := BuildEntityMergeStatement(EntityProps{
		Label:      "Function",
		Name:       "foo",
		LineNumber: 1,
	})
	if err == nil {
		t.Fatal("error = nil, want non-nil for empty file_path")
	}
	if !strings.Contains(err.Error(), "file_path must not be empty") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestMergeEntityExecutesStatement(t *testing.T) {
	t.Parallel()

	executor := &entityRecordingExecutor{}
	err := MergeEntity(context.Background(), executor, EntityProps{
		Label:      "Function",
		FilePath:   "/repo/main.go",
		Name:       "main",
		LineNumber: 1,
		UID:        "func:main",
	})
	if err != nil {
		t.Fatalf("MergeEntity() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (n:Function") {
		t.Fatalf("cypher missing Function MERGE: %s", executor.calls[0].Cypher)
	}
}

func TestMergeEntityRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := MergeEntity(context.Background(), nil, EntityProps{
		Label:      "Function",
		FilePath:   "/repo/main.go",
		Name:       "main",
		LineNumber: 1,
	})
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestMergeEntityPropagatesExecutorError(t *testing.T) {
	t.Parallel()

	executor := &entityRecordingExecutor{errAtCall: errors.New("neo4j down")}
	err := MergeEntity(context.Background(), executor, EntityProps{
		Label:      "Function",
		FilePath:   "/repo/main.go",
		Name:       "main",
		LineNumber: 1,
	})
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "neo4j down") {
		t.Fatalf("error = %q, want propagated error", err.Error())
	}
}

func TestValidateCypherLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label   string
		wantErr bool
	}{
		{"Function", false},
		{"K8sResource", false},
		{"_private", false},
		{"Bad Label", true},
		{"123invalid", true},
		{"label;inject", true},
		{"", true},
	}
	for _, tt := range tests {
		err := ValidateCypherLabel(tt.label)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateCypherLabel(%q) error = %v, wantErr = %v", tt.label, err, tt.wantErr)
		}
	}
}

func TestValidateCypherPropertyKeys(t *testing.T) {
	t.Parallel()

	if err := ValidateCypherPropertyKeys([]string{"name", "lang", "line_number"}); err != nil {
		t.Fatalf("error = %v for valid keys", err)
	}
	if err := ValidateCypherPropertyKeys([]string{"valid", "bad key"}); err == nil {
		t.Fatal("error = nil, want non-nil for invalid key")
	}
}

func TestSortedExtraKeys(t *testing.T) {
	t.Parallel()

	keys := sortedExtraKeys(map[string]any{
		"zebra": 1,
		"alpha": 2,
		"mid":   3,
	})
	if len(keys) != 3 {
		t.Fatalf("len = %d, want 3", len(keys))
	}
	if keys[0] != "alpha" || keys[1] != "mid" || keys[2] != "zebra" {
		t.Fatalf("keys = %v, want [alpha mid zebra]", keys)
	}
}

func TestSortedExtraKeysNil(t *testing.T) {
	t.Parallel()

	keys := sortedExtraKeys(nil)
	if keys != nil {
		t.Fatalf("keys = %v, want nil", keys)
	}
}

type entityRecordingExecutor struct {
	calls     []CypherStatement
	errAtCall error
}

func (r *entityRecordingExecutor) ExecuteCypher(_ context.Context, stmt CypherStatement) error {
	r.calls = append(r.calls, stmt)
	if r.errAtCall != nil {
		return r.errAtCall
	}
	return nil
}
