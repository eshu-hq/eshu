// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// contentFilesUpsertStatements are the Go constants that make up the two
// content_files upserts. Both must take their ON CONFLICT clause from the one
// shared constant: the F4 repo_id scan reads Go literals, so it sees that
// clause once, and only the shared constant keeps it equal to both statements.
var contentFilesUpsertStatements = []string{"upsertContentFileQuery", "upsertContentFileBatchSuffix"}

const contentFilesSharedClause = "upsertContentFileConflictClause"

// statementsMissingSharedClause reports each content_files upsert statement
// constant in src whose value does not reference the shared conflict clause
// constant, and each statement constant src does not declare at all.
func statementsMissingSharedClause(t *testing.T, filename, src string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	declared := map[string]bool{}
	var missing []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !isContentFilesUpsertStatement(name.Name) {
				continue
			}
			declared[name.Name] = true
			if i >= len(spec.Values) || !referencesIdent(spec.Values[i], contentFilesSharedClause) {
				missing = append(missing, name.Name+" does not use "+contentFilesSharedClause)
			}
		}
		return true
	})
	for _, name := range contentFilesUpsertStatements {
		if !declared[name] {
			missing = append(missing, name+" is not declared in "+filename)
		}
	}
	return missing
}

func isContentFilesUpsertStatement(name string) bool {
	for _, want := range contentFilesUpsertStatements {
		if name == want {
			return true
		}
	}
	return false
}

// referencesIdent reports whether expr is, or concatenates, the identifier.
func referencesIdent(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// TestContentFilesUpsertsShareOneConflictClause requires both content_files
// upserts to build their ON CONFLICT clause from the single shared constant,
// so the F4 repo_id scan of that one clause covers both statements (#8915).
func TestContentFilesUpsertsShareOneConflictClause(t *testing.T) {
	path := filepath.Join("..", "..", "content_writer_sql.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if missing := statementsMissingSharedClause(t, path, string(src)); len(missing) > 0 {
		t.Fatalf("content_files upserts drifted from the shared conflict clause:\n%v", missing)
	}
}

// TestSharedClauseScanSeededViolations is the RED/GREEN pair for the shared
// clause check and for the F4 scan of the clause as the shipped code spells it.
func TestSharedClauseScanSeededViolations(t *testing.T) {
	t.Parallel()
	const header = "package p\nconst upsertContentFileConflictClause = \"x\"\n"
	red := map[string]string{
		"batch suffix has its own clause": header + "const upsertContentFileQuery = \"INSERT\" + upsertContentFileConflictClause\nconst upsertContentFileBatchSuffix = \"ON CONFLICT (repo_id, relative_path) DO UPDATE SET content = EXCLUDED.content\"\n",
		"single query has its own clause": header + "const upsertContentFileQuery = \"INSERT ON CONFLICT\"\nconst upsertContentFileBatchSuffix = upsertContentFileConflictClause\n",
		"statement removed":               header + "const upsertContentFileQuery = \"INSERT\" + upsertContentFileConflictClause\n",
	}
	for name, src := range red {
		if len(statementsMissingSharedClause(t, name+".go", src)) == 0 {
			t.Errorf("%s: not reported", name)
		}
	}
	green := header + "const upsertContentFileQuery = \"INSERT\" + upsertContentFileConflictClause\nconst upsertContentFileBatchSuffix = upsertContentFileConflictClause\n"
	if found := statementsMissingSharedClause(t, "green.go", green); len(found) != 0 {
		t.Errorf("both statements use the clause: reported %v", found)
	}

	// The shipped clause is one Go raw literal: a repo_id assignment planted in
	// it must still be found by the F4 scan, and the clean clause must not be.
	plantedClause := "package p\nconst upsertContentFileConflictClause = `\nON CONFLICT (repo_id, relative_path) DO UPDATE\nSET repo_id = EXCLUDED.repo_id,\n    content = EXCLUDED.content\nWHERE content_files.content IS DISTINCT FROM EXCLUDED.content\n`\n"
	if found, upserts := scanSourceForRepoIDChanges(t, "planted.go", plantedClause); len(found) == 0 || upserts != 1 {
		t.Errorf("planted repo_id in the shared clause: violations %v, upserts %d, want a violation and one upsert", found, upserts)
	}
	cleanClause := "package p\nconst upsertContentFileConflictClause = `\nON CONFLICT (repo_id, relative_path) DO UPDATE\nSET content = EXCLUDED.content,\n    indexed_at = EXCLUDED.indexed_at\nWHERE (content_files.repo_id, content_files.content) IS DISTINCT FROM (EXCLUDED.repo_id, EXCLUDED.content)\n`\n"
	if found, upserts := scanSourceForRepoIDChanges(t, "clean.go", cleanClause); len(found) != 0 || upserts != 1 {
		t.Errorf("clean shared clause: violations %v, upserts %d, want none and one upsert", found, upserts)
	}
}
