package query

import (
	"testing"
)

func TestStringVal(t *testing.T) {
	row := map[string]any{
		"name":   "test-repo",
		"empty":  "",
		"nil":    nil,
		"number": 42,
	}

	if got := StringVal(row, "name"); got != "test-repo" {
		t.Errorf("StringVal(name) = %q, want %q", got, "test-repo")
	}
	if got := StringVal(row, "empty"); got != "" {
		t.Errorf("StringVal(empty) = %q, want %q", got, "")
	}
	if got := StringVal(row, "nil"); got != "" {
		t.Errorf("StringVal(nil) = %q, want %q", got, "")
	}
	if got := StringVal(row, "missing"); got != "" {
		t.Errorf("StringVal(missing) = %q, want %q", got, "")
	}
	if got := StringVal(row, "number"); got != "42" {
		t.Errorf("StringVal(number) = %q, want %q", got, "42")
	}
}

func TestBoolVal(t *testing.T) {
	row := map[string]any{
		"yes": true,
		"no":  false,
		"nil": nil,
		"str": "true",
	}

	if got := BoolVal(row, "yes"); !got {
		t.Error("BoolVal(yes) = false, want true")
	}
	if got := BoolVal(row, "no"); got {
		t.Error("BoolVal(no) = true, want false")
	}
	if got := BoolVal(row, "nil"); got {
		t.Error("BoolVal(nil) = true, want false")
	}
	if got := BoolVal(row, "missing"); got {
		t.Error("BoolVal(missing) = true, want false")
	}
	if got := BoolVal(row, "str"); got {
		t.Error("BoolVal(str) = true, want false (non-bool type)")
	}
}

func TestIntVal(t *testing.T) {
	row := map[string]any{
		"int64":   int64(42),
		"int":     99,
		"float64": float64(3.14),
		"nil":     nil,
		"str":     "hello",
	}

	if got := IntVal(row, "int64"); got != 42 {
		t.Errorf("IntVal(int64) = %d, want 42", got)
	}
	if got := IntVal(row, "int"); got != 99 {
		t.Errorf("IntVal(int) = %d, want 99", got)
	}
	if got := IntVal(row, "float64"); got != 3 {
		t.Errorf("IntVal(float64) = %d, want 3", got)
	}
	if got := IntVal(row, "nil"); got != 0 {
		t.Errorf("IntVal(nil) = %d, want 0", got)
	}
	if got := IntVal(row, "missing"); got != 0 {
		t.Errorf("IntVal(missing) = %d, want 0", got)
	}
}

func TestStringSliceVal(t *testing.T) {
	row := map[string]any{
		"strings": []string{"go", "python"},
		"any":     []any{"a", "b", "c"},
		"nil":     nil,
		"empty":   []any{},
	}

	got := StringSliceVal(row, "strings")
	if len(got) != 2 || got[0] != "go" || got[1] != "python" {
		t.Errorf("StringSliceVal(strings) = %v, want [go python]", got)
	}

	got = StringSliceVal(row, "any")
	if len(got) != 3 || got[0] != "a" {
		t.Errorf("StringSliceVal(any) = %v, want [a b c]", got)
	}

	got = StringSliceVal(row, "nil")
	if got != nil {
		t.Errorf("StringSliceVal(nil) = %v, want nil", got)
	}

	got = StringSliceVal(row, "empty")
	if len(got) != 0 {
		t.Errorf("StringSliceVal(empty) = %v, want []", got)
	}
}

func TestRepoRefFromRow(t *testing.T) {
	row := map[string]any{
		"id":         "repository:abc123",
		"name":       "my-repo",
		"path":       "/path/to/repo",
		"local_path": "/path/to/repo",
		"remote_url": "https://github.com/org/my-repo",
		"repo_slug":  "org/my-repo",
		"has_remote": true,
	}

	ref := RepoRefFromRow(row)
	if ref.ID != "repository:abc123" {
		t.Errorf("ID = %q, want %q", ref.ID, "repository:abc123")
	}
	if ref.Name != "my-repo" {
		t.Errorf("Name = %q, want %q", ref.Name, "my-repo")
	}
	if ref.LocalPath != "/path/to/repo" {
		t.Errorf("LocalPath = %q, want %q", ref.LocalPath, "/path/to/repo")
	}
	if ref.RemoteURL != "https://github.com/org/my-repo" {
		t.Errorf("RemoteURL = %q", ref.RemoteURL)
	}
	if !ref.HasRemote {
		t.Error("HasRemote = false, want true")
	}
}

func TestRepoRefFromRow_FallbackName(t *testing.T) {
	row := map[string]any{
		"id":         "",
		"local_path": "/repos/eshu",
	}

	ref := RepoRefFromRow(row)
	if ref.Name != "eshu" {
		t.Errorf("Name = %q, want derived from path", ref.Name)
	}
}

func TestRepoProjection(t *testing.T) {
	projection := RepoProjection("r")
	if projection == "" {
		t.Error("RepoProjection returned empty string")
	}
	// Should contain standard fields
	for _, field := range []string{"r.id", "r.name", "r.path", "local_path", "remote_url", "repo_slug", "has_remote"} {
		if !contains(projection, field) {
			t.Errorf("RepoProjection missing %q", field)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
