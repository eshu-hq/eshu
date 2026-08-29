// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSkipDropTargetCommentsEndsLineCommentAtBareCR is the #6268 regression
// for the SQL DROP-target recovery scanner. A `--` comment in a classic-Mac
// migration ends at a bare '\r'; a scan that only stops at '\n' swallows the
// whole remaining tail, so the recovered DROP targets after it are lost and
// the migration's graph truth is silently short.
func TestSkipDropTargetCommentsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	tail := ", public.users, -- between targets\r public.orgs RESTRICT;"
	targets, ok := parseDropTargetTail(tail)
	if !ok {
		t.Fatalf("parseDropTargetTail(%q) rejected a list whose comment ends at a bare CR", tail)
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "public.users", offset: strings.Index(tail, "public.users")},
		{name: "public.orgs", offset: strings.Index(tail, "public.orgs")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail(%q) = %#v, want %#v", tail, got, want)
	}
}

// TestSkipDropTargetCommentsKeepsCRLFTailIntact is the control: a CRLF tail
// already parsed before #6268 because the '\n' terminated the comment, so it
// must stay green on both sides of the fix.
func TestSkipDropTargetCommentsKeepsCRLFTailIntact(t *testing.T) {
	t.Parallel()

	tail := ", public.users, -- between targets\r\n public.orgs RESTRICT;"
	targets, ok := parseDropTargetTail(tail)
	if !ok {
		t.Fatalf("parseDropTargetTail(%q) rejected a CRLF list", tail)
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "public.users", offset: strings.Index(tail, "public.users")},
		{name: "public.orgs", offset: strings.Index(tail, "public.orgs")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail(%q) = %#v, want %#v", tail, got, want)
	}
}

// bareCRDropMigration is a classic-Mac migration: every line break is a bare
// '\r' and the file contains no '\n' at all. splitSQLStatements skips `--`
// comments before tree-sitter ever sees the statement, so a comment scan that
// ends only at '\n' runs to EOF and the DROP list loses every target after
// the comment -- the helper-level fix never reaches the real parse (#6268).
const bareCRDropMigration = "DROP TABLE public.users, -- between targets\r public.orgs;\rSELECT 1;\r"

// TestSplitSQLStatementsEndsLineCommentAtBareCR drives the production
// segmenter rather than the DROP-tail helper. When the `--` scan swallows the
// rest of the file the trailing `;` is never seen, so the whole migration
// collapses into one unterminated segment and the recovery path is handed a
// truncated DROP list.
func TestSplitSQLStatementsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	segments := splitSQLStatements(bareCRDropMigration)
	if len(segments) != 2 {
		t.Fatalf("splitSQLStatements(%q) produced %d segment(s), want 2: %#v", bareCRDropMigration, len(segments), segments)
	}
	if !strings.HasSuffix(segments[0].text, "public.orgs;") {
		t.Fatalf("splitSQLStatements(%q)[0] = %q, want it to end at the `;` after public.orgs", bareCRDropMigration, segments[0].text)
	}
}

// TestParseBareCRMigrationKeepsEveryDropTarget is the end-to-end regression
// the helper test could not catch: the real Parse entry point must recover
// both DROP targets, and stamp each with its own bare-CR source line rather
// than collapsing them onto line 1.
func TestParseBareCRMigrationKeepsEveryDropTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_pair", "migration.sql"),
		bareCRDropMigration,
	)
	got := parseSQLTestFile(t, path)

	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.users", "drop", 1)
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.orgs", "drop", 2)
}

// TestParseCRLFMigrationKeepsEveryDropTarget is the control: a CRLF migration
// already parsed before #6268 and must keep both targets on their own lines
// after it, proving the terminator change did not renumber ordinary files.
func TestParseCRLFMigrationKeepsEveryDropTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_pair_crlf", "migration.sql"),
		"DROP TABLE public.users, -- between targets\r\n public.orgs;\r\nSELECT 1;\r\n",
	)
	got := parseSQLTestFile(t, path)

	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.users", "drop", 1)
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.orgs", "drop", 2)
}

// TestSplitSQLStatementsRecoversLineInitialCreateAfterBareCR covers the
// sibling of skipLineComment in the same segmenter. When an earlier malformed
// statement leaves a paren open, only a line-initial CREATE/ALTER is a
// recovery boundary -- and atLineStart counted a bare '\r' as ordinary
// horizontal whitespace, so on a classic-Mac file it walked straight past the
// line break and judged every CREATE mid-line. The second table was then
// swallowed by the first statement's unbalanced parens (#6268).
func TestSplitSQLStatementsRecoversLineInitialCreateAfterBareCR(t *testing.T) {
	t.Parallel()

	source := "CREATE TABLE public.a (id INT\rCREATE TABLE public.b (id INT);\r"
	segments := splitSQLStatements(source)
	if len(segments) != 2 {
		t.Fatalf("splitSQLStatements(%q) produced %d segment(s), want 2: %#v", source, len(segments), segments)
	}
	if !strings.HasPrefix(segments[1].text, "CREATE TABLE public.b") {
		t.Fatalf("splitSQLStatements(%q)[1] = %q, want the recovered public.b statement", source, segments[1].text)
	}
}
