// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// selectColumns returns the select-list expressions of a statement, in order,
// with whitespace folded.
func selectColumns(t *testing.T, statement string) []string {
	t.Helper()
	match := regexp.MustCompile(`(?s)SELECT\s+(.*?)\s+FROM\s+content_files`).FindStringSubmatch(statement)
	if match == nil {
		t.Fatalf("statement has no SELECT ... FROM content_files: %q", statement)
	}
	// Split on commas outside parentheses: coalesce(commit_sha, '') is one column.
	var (
		columns []string
		depth   int
		current strings.Builder
	)
	flush := func() {
		columns = append(columns, strings.Join(strings.Fields(current.String()), " "))
		current.Reset()
	}
	for _, r := range match[1] {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return columns
}

// TestFileStatementsShareOneColumnList pins the contract the single row mapper
// relies on: the list, single-row and batched statements select the same eight
// columns in the same order, and only the list statement replaces content with
// an empty string.
func TestFileStatementsShareOneColumnList(t *testing.T) {
	t.Parallel()

	get := selectColumns(t, GetFileStatement)
	batch := selectColumns(t, GetFilesStatement)
	list := selectColumns(t, ListStatement)
	if !reflect.DeepEqual(get, batch) {
		t.Fatalf("GetFilesStatement columns %v, want GetFileStatement columns %v", batch, get)
	}
	if len(get) != len(FileScanTargets(&File{})) {
		t.Fatalf("statements select %d columns, FileScanTargets maps %d", len(get), len(FileScanTargets(&File{})))
	}
	if len(list) != len(get) {
		t.Fatalf("ListStatement selects %d columns, want %d", len(list), len(get))
	}
	for i := range get {
		want := get[i]
		if want == "content" {
			want = "''"
		}
		if list[i] != want {
			t.Errorf("ListStatement column %d = %q, want %q", i, list[i], want)
		}
	}
	if !strings.Contains(ListStatement, "ORDER BY relative_path") {
		t.Error("ListStatement must order by relative_path: the first-N windows depend on it")
	}
	if got := len(RelativePathScanTargets(new(string))); got != 1 {
		t.Fatalf("RelativePathScanTargets maps %d columns, want 1", got)
	}
}

// TestFileScanTargetsFillEveryField proves the mapper writes each column into
// the field of the same name, in statement order.
func TestFileScanTargetsFillEveryField(t *testing.T) {
	t.Parallel()

	var file File
	values := []any{"repo", "a/b.tf", "sha", "body", "hash", 7, "hcl", "terraform"}
	targets := FileScanTargets(&file)
	for i, target := range targets {
		reflect.ValueOf(target).Elem().Set(reflect.ValueOf(values[i]))
	}
	want := File{
		RepoID: "repo", RelativePath: "a/b.tf", CommitSHA: "sha", Content: "body",
		ContentHash: "hash", LineCount: 7, Language: "hcl", ArtifactType: "terraform",
	}
	if file != want {
		t.Fatalf("mapped file = %+v, want %+v", file, want)
	}
}

// TestSpaceCharactersAreTheSetTrimSpaceTrims proves the blank predicate's
// character set is exactly the runes strings.TrimSpace removes.
func TestSpaceCharactersAreTheSetTrimSpaceTrims(t *testing.T) {
	t.Parallel()

	set := SpaceCharacters()
	for _, r := range set {
		if strings.TrimSpace(string(r)) != "" {
			t.Errorf("U+%04X is in the set but TrimSpace keeps it", r)
		}
	}
	for _, r := range []rune{0xFEFF, 0x200B, 'a', '0'} {
		if strings.ContainsRune(set, r) {
			t.Errorf("U+%04X is in the set but TrimSpace keeps it", r)
		}
	}
	if !strings.ContainsRune(set, ' ') || !strings.ContainsRune(set, 0xA0) {
		t.Error("the set must hold the space and the no-break space")
	}
}
