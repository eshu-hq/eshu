// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Source guards for two limits of the migration 184 fence (ruling
// arb-8155-f1, findings F3 and F4):
//
//   - session_replication_role = replica skips every ordinary trigger, the
//     fence included. No Eshu binary may set it.
//   - The UPDATE trigger reads NEW TABLE only, so an UPDATE that changes
//     repo_id bumps the new repository and not the old one. No Go statement
//     may change content_files.repo_id: both content_files upserts must leave
//     repo_id out of their SET list, and no Go UPDATE content_files may set it.

var (
	// replicaRolePattern finds any mention of the setting in Go source.
	replicaRolePattern = regexp.MustCompile(`(?i)session_replication_role`)
	// contentFilesUpdatePattern is an UPDATE of content_files.
	contentFilesUpdatePattern = regexp.MustCompile(`(?i)\bUPDATE\s+(?:ONLY\s+)?` + tableRef("content_files"))
	// contentFilesUpsertPattern is the content_files conflict target with a DO
	// UPDATE action; the batch path keeps it in a suffix literal of its own.
	contentFilesUpsertPattern = regexp.MustCompile(`(?i)\bON\s+CONFLICT\s*\(\s*repo_id\s*,\s*relative_path\s*\)\s*DO\s+UPDATE\s+SET\b`)
	// repoIDAssignPattern is a SET item that assigns repo_id.
	repoIDAssignPattern = regexp.MustCompile(`(?i)^(?:"?\w+"?\.)?"?repo_id"?\s*=`)
	// repoIDColumnPattern is repo_id in a row-constructor column list.
	repoIDColumnPattern = regexp.MustCompile(`(?i)(?:^|[\s,(])"?repo_id"?(?:$|[\s,)])`)
)

// repoIDChangeViolations reports each statement in text that is a
// content_files UPDATE or a content_files upsert assigning repo_id, and how
// many content_files upserts it saw.
func repoIDChangeViolations(origin, text string) (violations []string, upserts int) {
	text = sqlCommentPattern.ReplaceAllString(text, " ")
	for _, statement := range strings.Split(text, ";") {
		statement = strings.TrimSpace(statementWhitespace.ReplaceAllString(statement, " "))
		isUpsert := contentFilesUpsertPattern.MatchString(statement)
		if isUpsert {
			upserts++
		}
		if !isUpsert && !contentFilesUpdatePattern.MatchString(statement) {
			continue
		}
		for _, setLoc := range setPattern.FindAllStringIndex(statement, -1) {
			for _, item := range setListItems(statement, setLoc[1]) {
				item = strings.TrimSpace(item)
				row := rowSetPattern.FindStringSubmatch(item)
				if repoIDAssignPattern.MatchString(item) || (row != nil && repoIDColumnPattern.MatchString(row[1])) {
					violations = append(violations, fmt.Sprintf("%s: statement changes content_files.repo_id: %.160s", origin, statement))
				}
			}
		}
	}
	return violations, upserts
}

// nonTestGoFiles calls visit with the path and source of every non-test Go
// file in the module.
func nonTestGoFiles(t *testing.T, visit func(path, src string)) int {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	files := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".gocache") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		visit(path, string(src))
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if files < 1000 {
		t.Fatalf("scanned %d Go files, want at least 1000: the walk root is wrong", files)
	}
	return files
}

// TestNoGoCodeSetsSessionReplicationRole fails on any non-test Go mention of
// session_replication_role (F3).
func TestNoGoCodeSetsSessionReplicationRole(t *testing.T) {
	var hits []string
	nonTestGoFiles(t, func(path, src string) {
		if replicaRolePattern.MatchString(src) {
			hits = append(hits, path)
		}
	})
	if len(hits) > 0 {
		t.Fatalf("non-test Go source names session_replication_role, which skips the version fence: %v", hits)
	}
}

// TestNoGoStatementChangesContentFilesRepoID fails on a Go statement that
// changes content_files.repo_id (F4), and requires that the scan saw both
// content_files upserts (the single-row and the batch suffix).
func TestNoGoStatementChangesContentFilesRepoID(t *testing.T) {
	var violations []string
	upserts := 0
	nonTestGoFiles(t, func(path, src string) {
		if !strings.Contains(src, "content_files") && !strings.Contains(src, "relative_path") {
			return
		}
		for _, text := range goLiteralSQL(t, path, src) {
			found, n := repoIDChangeViolations(path, text)
			violations = append(violations, found...)
			upserts += n
		}
	})
	if upserts < 2 {
		t.Fatalf("saw %d content_files upserts, want at least 2: the scan is broken", upserts)
	}
	if len(violations) > 0 {
		t.Fatalf("statements that change content_files.repo_id:\n%s", strings.Join(violations, "\n"))
	}
}

// TestFenceSourceGuardSeededViolations is the RED/GREEN pair for both guards.
func TestFenceSourceGuardSeededViolations(t *testing.T) {
	t.Parallel()
	red := map[string]string{
		"update sets repo_id":    "UPDATE content_files SET repo_id = $1 WHERE relative_path = $2",
		"update row constructor": "UPDATE content_files SET (repo_id, content) = ($1, $2)",
		"upsert sets repo_id":    "INSERT INTO content_files (repo_id) VALUES ($1) ON CONFLICT (repo_id, relative_path) DO UPDATE SET repo_id = EXCLUDED.repo_id",
		"qualified":              "UPDATE public.content_files AS f SET f.repo_id = 'b'",
	}
	for name, text := range red {
		if found, _ := repoIDChangeViolations("seed", text); len(found) == 0 {
			t.Errorf("%s: not reported", name)
		}
	}
	green := map[string]string{
		"shipped upsert": "ON CONFLICT (repo_id, relative_path) DO UPDATE SET content = EXCLUDED.content, indexed_at = EXCLUDED.indexed_at",
		"update content": "UPDATE content_files SET content = $1 WHERE repo_id = $2",
		"other table":    "UPDATE content_entities SET repo_id = $1",
	}
	for name, text := range green {
		if found, _ := repoIDChangeViolations("seed", text); len(found) != 0 {
			t.Errorf("%s: reported %v", name, found)
		}
	}
	if !replicaRolePattern.MatchString(`db.Exec("SET session_replication_role = replica")`) {
		t.Error("replica-role pattern misses a SET")
	}
}
