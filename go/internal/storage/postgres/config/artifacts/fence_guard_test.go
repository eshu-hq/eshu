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

// Source guards for three limits of the migration 184 fence (ruling
// arb-8155-f1, findings F3 and F4, and the #8282 review):
//
//   - session_replication_role = replica skips every ordinary trigger, the
//     fence included. No Eshu binary may set it.
//   - SET CONSTRAINTS ... IMMEDIATE runs the deferred commit trigger early, so
//     a transaction takes version rows before its later content locks and
//     reopens the deadlock cycle the commit-time fence closed. No Eshu binary
//     may issue SET CONSTRAINTS.
//   - The UPDATE trigger reads NEW TABLE only, so an UPDATE that changes
//     repo_id bumps the new repository and not the old one. No Go statement
//     may change content_files.repo_id: both content_files upserts must leave
//     repo_id out of their SET list, and no Go UPDATE content_files may set it.

var (
	// replicaRolePattern finds any mention of the setting in Go source.
	replicaRolePattern = regexp.MustCompile(`(?i)session_replication_role`)
	// immediateConstraintsPattern finds a SET CONSTRAINTS statement in Go
	// source.
	immediateConstraintsPattern = regexp.MustCompile(`(?i)\bSET\s+CONSTRAINTS\b`)
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

// setConstraintsStatements reports each SQL text, read from one Go literal
// expression as the F4 guard reads it, that issues SET CONSTRAINTS once SQL
// comments are stripped. A Go comment never reaches it.
func setConstraintsStatements(origin string, texts []string) []string {
	var hits []string
	for _, text := range texts {
		text = statementWhitespace.ReplaceAllString(sqlCommentPattern.ReplaceAllString(text, " "), " ")
		if immediateConstraintsPattern.MatchString(text) {
			hits = append(hits, fmt.Sprintf("%s: %.160s", origin, strings.TrimSpace(text)))
		}
	}
	return hits
}

// scanSourceForSetConstraints runs the SET CONSTRAINTS guard over one Go file.
// The cheap prefilter reads goLiteralSQL's joined literal runs, not the raw
// source, so a keyword split across concatenated literals still reaches the
// scan.
func scanSourceForSetConstraints(t *testing.T, path, src string) []string {
	t.Helper()
	texts := goLiteralSQL(t, path, src)
	if !strings.Contains(strings.ToUpper(strings.Join(texts, "\x00")), "CONSTRAINTS") {
		return nil
	}
	return setConstraintsStatements(path, texts)
}

// scanSourceForRepoIDChanges runs the F4 guard over one Go file and returns
// its violations and the content_files upserts it saw.
func scanSourceForRepoIDChanges(t *testing.T, path, src string) (violations []string, upserts int) {
	t.Helper()
	texts := goLiteralSQL(t, path, src)
	joined := strings.ToLower(strings.Join(texts, "\x00"))
	if !strings.Contains(joined, "content_files") && !strings.Contains(joined, "relative_path") {
		return nil, 0
	}
	for _, text := range texts {
		found, n := repoIDChangeViolations(path, text)
		violations = append(violations, found...)
		upserts += n
	}
	return violations, upserts
}

// TestNoGoCodeSetsConstraintsImmediate fails on any non-test Go statement that
// issues SET CONSTRAINTS, which would run the fence's deferred commit trigger
// before the transaction's last content lock (#8282 review).
func TestNoGoCodeSetsConstraintsImmediate(t *testing.T) {
	var hits []string
	nonTestGoFiles(t, func(path, src string) {
		hits = append(hits, scanSourceForSetConstraints(t, path, src)...)
	})
	if len(hits) > 0 {
		t.Fatalf("non-test Go statements issue SET CONSTRAINTS, which voids the fence's commit-time lock order:\n%s",
			strings.Join(hits, "\n"))
	}
}

// TestNoGoStatementChangesContentFilesRepoID fails on a Go statement that
// changes content_files.repo_id (F4), and requires that the scan saw both
// content_files upserts (the single-row and the batch suffix).
func TestNoGoStatementChangesContentFilesRepoID(t *testing.T) {
	var violations []string
	upserts := 0
	nonTestGoFiles(t, func(path, src string) {
		found, n := scanSourceForRepoIDChanges(t, path, src)
		violations = append(violations, found...)
		upserts += n
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
	splitRed := map[string]string{
		"split SET CONSTRAINTS": "package p\nconst q = \"SET CONSTRA\" + \"INTS ALL IMMEDIATE\"\n",
		"split content_files":   "package p\nconst q = \"UPDATE content_\" + \"files SET repo_id = $1\"\n",
	}
	for name, src := range splitRed {
		constraints := scanSourceForSetConstraints(t, name+".go", src)
		repoIDs, _ := scanSourceForRepoIDChanges(t, name+".go", src)
		if len(constraints)+len(repoIDs) == 0 {
			t.Errorf("%s: a keyword split across Go literals evades the guard's prefilter", name)
		}
	}
	splitGreen := map[string]string{
		"go comment":  "package p\n// SET CONSTRAINTS ALL IMMEDIATE; UPDATE content_files SET repo_id = 1\nconst q = \"SELECT 1\"\n",
		"sql comment": "package p\nconst q = \"-- SET CONSTRA\" + \"INTS ALL IMMEDIATE\\nSELECT 1\"\n",
		"column name": "package p\nconst q = \"UPDATE t SET constraint_\" + \"name = $1\"\n",
	}
	for name, src := range splitGreen {
		constraints := scanSourceForSetConstraints(t, name+".go", src)
		repoIDs, _ := scanSourceForRepoIDChanges(t, name+".go", src)
		if len(constraints)+len(repoIDs) != 0 {
			t.Errorf("%s: reported %v %v", name, constraints, repoIDs)
		}
	}
	constraintsRed := map[string]string{
		"literal":      "package p\nconst q = \"SET CONSTRAINTS ALL IMMEDIATE\"\n",
		"concatenated": "package p\nconst q = \"set \" + \"constraints config_artifact_fence_pending_commit immediate\"\n",
	}
	for name, src := range constraintsRed {
		if len(setConstraintsStatements(name, goLiteralSQL(t, name+".go", src))) == 0 {
			t.Errorf("SET CONSTRAINTS %s: not reported", name)
		}
	}
	constraintsGreen := map[string]string{
		"go comment":  "package p\n// Never SET CONSTRAINTS ALL IMMEDIATE here.\nconst q = \"SELECT 1\"\n",
		"sql comment": "package p\nconst q = \"-- SET CONSTRAINTS ALL IMMEDIATE\\nSELECT 1\"\n",
		"column name": "package p\nconst q = \"UPDATE t SET constraint_name = $1\"\n",
	}
	for name, src := range constraintsGreen {
		if found := setConstraintsStatements(name, goLiteralSQL(t, name+".go", src)); len(found) != 0 {
			t.Errorf("SET CONSTRAINTS %s: reported %v", name, found)
		}
	}
}
