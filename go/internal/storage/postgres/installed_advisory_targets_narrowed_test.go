// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// parseEscapeStringLiteral decodes the Postgres escape-string literal
// osPackagePURLTrimSet into its runes: E'...' with \t, \n, \f, \r and \uXXXX.
func parseEscapeStringLiteral(t *testing.T, literal string) []rune {
	t.Helper()
	body, ok := strings.CutPrefix(literal, "E'")
	if !ok || !strings.HasSuffix(body, "'") {
		t.Fatalf("trim set %q is not an E'...' literal", literal)
	}
	body = strings.TrimSuffix(body, "'")
	var out []rune
	for i := 0; i < len(body); {
		if body[i] != '\\' {
			r := []rune(body[i:])[0]
			out = append(out, r)
			i += len(string(r))
			continue
		}
		switch body[i+1] {
		case 't':
			out, i = append(out, '\t'), i+2
		case 'n':
			out, i = append(out, '\n'), i+2
		case 'f':
			out, i = append(out, '\f'), i+2
		case 'r':
			out, i = append(out, '\r'), i+2
		case 'u':
			value, err := strconv.ParseUint(body[i+2:i+6], 16, 32)
			if err != nil {
				t.Fatalf("bad \\u escape in %q: %v", literal, err)
			}
			if value > utf8.MaxRune {
				t.Fatalf("\\u escape %q in %q is not a valid rune", body[i+2:i+6], literal)
				return nil
			}
			out, i = append(out, rune(value)), i+6
		default:
			t.Fatalf("unsupported escape \\%c in %q", body[i+1], literal)
		}
	}
	return out
}

// TestOSPackagePURLTrimSetMatchesGoTrimSpace proves the SQL btrim character set
// is exactly the runes strings.TrimSpace strips. The reducer derives an
// installed package id with TrimSpace; a SQL set missing a rune (for example
// U+00A0) would fail to match a padded purl the Go matcher accepts, and the
// narrowed read would miss a finding the full drain finds.
func TestOSPackagePURLTrimSetMatchesGoTrimSpace(t *testing.T) {
	t.Parallel()

	got := parseEscapeStringLiteral(t, osPackagePURLTrimSet)
	var want []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			want = append(want, r)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("btrim set = %U, want every unicode.IsSpace rune %U", got, want)
	}
	for _, r := range want {
		if trimmed := strings.TrimSpace(string(r) + "x" + string(r)); trimmed != "x" {
			t.Fatalf("strings.TrimSpace does not strip %U (got %q); the enumeration above no longer models it", r, trimmed)
		}
	}
}

// TestOSPackagePURLPrefixExpressionMatchesMigration pins the query expression to
// the expression index of migration 153: an index on any other spelling is not
// used, so the narrowed read would silently fall back to scanning every
// installed package.
func TestOSPackagePURLPrefixExpressionMatchesMigration(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("migrations/153_fact_records_os_package_purl_prefix_idx.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !strings.Contains(string(raw), osPackagePURLPrefixExpression("payload")) {
		t.Fatalf("migration 153 does not contain the query's index expression:\n%s\nmigration:\n%s",
			osPackagePURLPrefixExpression("payload"), raw)
	}
	if !strings.Contains(listOSPackageAdvisoryTargetsForPackagesQuery(), osPackagePURLPrefixExpression("fact.payload")) {
		t.Fatal("the narrowed query does not use the index expression")
	}
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_purl_prefix_idx",
		"WHERE fact_kind = 'vulnerability.os_package'",
		"AND is_tombstone = FALSE",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("migration 153 missing %q", want)
		}
	}
}

// osPackageAdvisoryTargetColumns is the projection order both OS package
// target queries return and both row scans read (the narrowed scan appends
// the active flag after them). A column added to the struct must extend both
// SELECTs and both scans in step, or the row silently shifts.
func osPackageAdvisoryTargetColumns(prefix string) []string {
	columns := []string{
		"ecosystem",
		"distro",
		"distro_version",
		"package_name",
		"installed_version",
		"package_manager",
		"arch",
		"vendor_advisory_source",
		"repository_class",
		"purl",
		"source_package",
		"fact_id",
		"scope_id",
		"generation_id",
	}
	out := make([]string, 0, len(columns))
	for i, column := range columns {
		line := prefix + column
		if i < len(columns)-1 {
			line += ","
		}
		out = append(out, line)
	}
	return out
}

// outerSelectLines returns the trimmed lines of the last SELECT block before
// marker (the outer projection of a two-level query).
func outerSelectLines(t *testing.T, name, query, marker string) []string {
	t.Helper()
	from := strings.Index(query, marker)
	if from < 0 {
		t.Fatalf("%s query lost %q", name, marker)
	}
	head := query[:from]
	selectAt := strings.LastIndex(head, "SELECT")
	if selectAt < 0 {
		t.Fatalf("%s query lost its outer SELECT", name)
	}
	var lines []string
	for _, line := range strings.Split(head[selectAt+len("SELECT"):], "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// TestOSPackageAdvisoryTargetColumnsStayInLockstep pins the projection order
// of the rotation and narrowed reads against the struct scan order: a column
// reordered in one query but not the scans would shift every field after it.
func TestOSPackageAdvisoryTargetColumnsStayInLockstep(t *testing.T) {
	t.Parallel()

	rotation := outerSelectLines(t, "rotation", listOSPackageAdvisoryTargetsQuery(), "FROM rotated_targets")
	for i, want := range osPackageAdvisoryTargetColumns("") {
		if i >= len(rotation) || rotation[i] != want {
			got := "<missing>"
			if i < len(rotation) {
				got = rotation[i]
			}
			t.Fatalf("rotation projection line %d = %q, want %q", i, got, want)
		}
	}
	narrowed := outerSelectLines(t, "narrowed", listOSPackageAdvisoryTargetsForPackagesQuery(), "FROM candidates")
	narrowedCoalesce := []string{
		`LOWER((COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro')) COLLATE "C") COLLATE "default" AS ecosystem,`,
		`COALESCE(c.payload->>'distro', '') AS distro,`,
		`COALESCE(c.payload->>'distro_version', '') AS distro_version,`,
		`COALESCE(c.payload->>'name', '') AS package_name,`,
		`COALESCE(c.payload->>'installed_version_raw', '') AS installed_version,`,
		`COALESCE(c.payload->>'package_manager', '') AS package_manager,`,
		`COALESCE(c.payload->>'arch', '') AS arch,`,
		`COALESCE(c.payload->>'vendor_advisory_source', '') AS vendor_advisory_source,`,
		`COALESCE(c.payload->>'repository_class', '') AS repository_class,`,
		`COALESCE(c.payload->>'purl', '') AS purl,`,
		`COALESCE(c.payload->>'source_package', '') AS source_package,`,
		`c.fact_id,`,
		`c.scope_id,`,
		`c.generation_id,`,
	}
	for i, want := range narrowedCoalesce {
		if i >= len(narrowed) || narrowed[i] != want {
			got := "<missing>"
			if i < len(narrowed) {
				got = narrowed[i]
			}
			t.Fatalf("narrowed projection line %d = %q, want %q", i, got, want)
		}
	}
}

// TestOSPackageSourcePURLPrefixExpressionMatchesMigration pins the source
// expression to the expression index of migration 273: an index on any other
// spelling would not serve the narrowed read's OR branch, which would
// silently fall back to scanning every installed package for source-joined
// advisories (#9859).
func TestOSPackageSourcePURLPrefixExpressionMatchesMigration(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("migrations/273_fact_records_os_package_source_purl_prefix_idx.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !strings.Contains(string(raw), osPackageSourcePURLPrefixExpression("payload")) {
		t.Fatalf("migration 273 does not contain the query's source index expression:\n%s\nmigration:\n%s",
			osPackageSourcePURLPrefixExpression("payload"), raw)
	}
	if !strings.Contains(listOSPackageAdvisoryTargetsForPackagesQuery(), osPackageSourcePURLPrefixExpression("fact.payload")) {
		t.Fatal("the narrowed query does not use the source index expression")
	}
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_source_purl_prefix_idx",
		"WHERE fact_kind = 'vulnerability.os_package'",
		"AND is_tombstone = FALSE",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("migration 273 missing %q", want)
		}
	}
}

// TestOSPackageNarrowedQueryShape pins the properties the plan depends on and
// the predicates the rotation reader shares.
func TestOSPackageNarrowedQueryShape(t *testing.T) {
	t.Parallel()

	narrowed := listOSPackageAdvisoryTargetsForPackagesQuery()
	rotation := listOSPackageAdvisoryTargetsQuery()
	for _, want := range []string{
		"WITH candidates AS MATERIALIZED", // without it the planner drives from the scope join
		"fact.fact_kind = 'vulnerability.os_package'",
		"fact.is_tombstone = FALSE",
		"= ANY($1::text[])",
		"fact.fact_id > $2",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $3",
		"= ANY($4::text[])) AS active",
		"generation.status",
		// #9859: the candidates CTE is a UNION of two single-arm keyset
		// sub-pages (binary arm, source arm). A bare OR across the two
		// expression predicates makes the generic plan cost the
		// primary-key scan as cheaper than either expression index, so
		// the read filters every installed package per page.
		"UNION",
		osPackagePURLPrefixExpression("fact.payload"),
		osPackageSourcePURLPrefixExpression("fact.payload"),
	} {
		if !strings.Contains(narrowed, want) {
			t.Fatalf("narrowed query missing %q:\n%s", want, narrowed)
		}
	}
	// UNION, never UNION ALL: a row whose binary name equals its source name
	// is a candidate of both arms and must drain exactly once.
	if strings.Contains(narrowed, "UNION ALL") {
		t.Fatalf("narrowed query uses UNION ALL; a both-arm row would drain twice:\n%s", narrowed)
	}
	// Each arm pages on its own cursor: one fact_id bound per arm plus the
	// shared outer page bound. A single cursor would let one arm's rows
	// starve the other's.
	if got := strings.Count(narrowed, "fact.fact_id > $2"); got != 2 {
		t.Fatalf("narrowed query has %d per-arm cursors, want 2 (one binary arm, one source arm):\n%s", got, narrowed)
	}
	// No bare OR joining the arms: any OR in this query is the arm join the
	// UNION replaced. The CASE rejection set is AND-only by construction.
	if strings.Contains(narrowed, " OR ") {
		t.Fatalf("narrowed query joins the arms with OR; each arm must be a single sargable predicate:\n%s", narrowed)
	}
	for _, banned := range []string{"ROW_NUMBER", "COUNT(*) OVER", "MOD("} {
		if strings.Contains(narrowed, banned) {
			t.Fatalf("narrowed query contains %q; a window forces a full read and sort per page", banned)
		}
	}
	// The ecosystem predicate is the rotation reader's, so a paged drain and the
	// planner see the same rows.
	ecosystem := "LOWER((COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro')) COLLATE \"C\") COLLATE \"default\" = ANY($4::text[])"
	if !strings.Contains(narrowed, ecosystem) {
		t.Fatalf("narrowed query lost the ecosystem predicate %q", ecosystem)
	}
	if !strings.Contains(rotation, "LOWER((COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) COLLATE \"C\") COLLATE \"default\" = ANY($1::text[])") {
		t.Fatal("rotation query changed its ecosystem predicate; update the narrowed query in step")
	}
}

func osPackageCandidateRow(factID string, active, complete bool) []any {
	version := "1.0-1"
	if !complete {
		version = ""
	}
	return []any{
		"debian", "debian", "12", "pkg", version, "dpkg", "amd64", "debian", "vendor", "pkg:deb/debian/pkg", "",
		factID, "scan-scope", "scan-gen", active,
	}
}

func osPackageCandidatePage(from, count int, active bool) queueFakeRows {
	rows := make([][]any, 0, count)
	for i := range count {
		rows = append(rows, osPackageCandidateRow(fmt.Sprintf("fact-%06d", from+i), active, true))
	}
	return queueFakeRows{rows: rows}
}

// TestListOSPackageAdvisoryFactEnvelopesDrainsInOneSnapshot pins #7154: the
// whole drain reads one repeatable-read snapshot (a scan generation flip
// between page statements would move os_package fact ids, which hash the
// generation id, and the cursor would skip installed packages), advances the
// cursor from the last candidate, and stops on the first short page.
func TestListOSPackageAdvisoryFactEnvelopesDrainsInOneSnapshot(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		osPackageCandidatePage(0, osPackageAdvisoryDrainPageSize, true),
		osPackageCandidatePage(500, osPackageAdvisoryDrainPageSize, true),
		osPackageCandidatePage(1000, 203, true),
	}}
	envelopes, skipped, truncated, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
		context.Background(), []string{"debian"}, []string{"pkg:deb/debian/pkg"}, 100_000,
	)
	if err != nil {
		t.Fatalf("ListOSPackageAdvisoryFactEnvelopes() error = %v", err)
	}
	if len(envelopes) != 1203 || skipped != 0 || truncated {
		t.Fatalf("envelopes=%d skipped=%d truncated=%v, want 1203/0/false", len(envelopes), skipped, truncated)
	}
	if database.beginReadOnlyRepeatableReadCalls != 1 || database.transactionCommitCalls != 1 {
		t.Fatalf("snapshots=%d commits=%d, want the whole drain in exactly one committed snapshot",
			database.beginReadOnlyRepeatableReadCalls, database.transactionCommitCalls)
	}
	if len(database.queries) != 3 {
		t.Fatalf("page statements = %d, want 3", len(database.queries))
	}
	wantCursors := []string{"", "fact-000499", "fact-000999"}
	for i, want := range wantCursors {
		if got := database.queries[i].args[1]; got != want {
			t.Fatalf("page %d cursor = %v, want %q", i, got, want)
		}
	}
	if got := database.queries[0].args[2]; got != osPackageAdvisoryDrainPageSize {
		t.Fatalf("page size arg = %v, want %d", got, osPackageAdvisoryDrainPageSize)
	}
}

// TestListOSPackageAdvisoryFactEnvelopesStopsOnTheLimitKeepingTheCrossingPage
// pins the budget contract: once the envelopes held exceed the caller's limit the
// drain stops and reports truncated, and the page that crossed is kept.
func TestListOSPackageAdvisoryFactEnvelopesStopsOnTheLimitKeepingTheCrossingPage(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		osPackageCandidatePage(0, osPackageAdvisoryDrainPageSize, true),
		osPackageCandidatePage(500, osPackageAdvisoryDrainPageSize, true),
	}}
	envelopes, _, truncated, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
		context.Background(), []string{"debian"}, []string{"pkg:deb/debian/pkg"}, 100,
	)
	if err != nil {
		t.Fatalf("ListOSPackageAdvisoryFactEnvelopes() error = %v", err)
	}
	if !truncated || len(envelopes) != osPackageAdvisoryDrainPageSize || len(database.queries) != 1 {
		t.Fatalf("truncated=%v envelopes=%d pages=%d, want truncated after the one crossing page of %d",
			truncated, len(envelopes), len(database.queries), osPackageAdvisoryDrainPageSize)
	}
}

// TestListOSPackageAdvisoryFactEnvelopesInactiveRowsDoNotEndTheDrain pins that a
// full page of candidates whose generation is inactive still advances the cursor
// and does not end the drain: only a short candidate page is the last one.
func TestListOSPackageAdvisoryFactEnvelopesInactiveRowsDoNotEndTheDrain(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		osPackageCandidatePage(0, osPackageAdvisoryDrainPageSize, false),
		osPackageCandidatePage(500, 7, true),
	}}
	envelopes, skipped, truncated, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
		context.Background(), []string{"debian"}, []string{"pkg:deb/debian/pkg"}, 100_000,
	)
	if err != nil {
		t.Fatalf("ListOSPackageAdvisoryFactEnvelopes() error = %v", err)
	}
	if len(envelopes) != 7 || skipped != 0 || truncated || len(database.queries) != 2 {
		t.Fatalf("envelopes=%d skipped=%d truncated=%v pages=%d, want 7/0/false/2", len(envelopes), skipped, truncated, len(database.queries))
	}
}

// TestListOSPackageAdvisoryFactEnvelopesCountsSkippedRows pins that an active
// row missing a required field is skipped and counted, not dropped silently.
func TestListOSPackageAdvisoryFactEnvelopesCountsSkippedRows(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		osPackageCandidateRow("fact-1", true, true),
		osPackageCandidateRow("fact-2", true, false),
	}}}}
	envelopes, skipped, _, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
		context.Background(), []string{"debian"}, []string{"pkg:deb/debian/pkg"}, 100,
	)
	if err != nil || len(envelopes) != 1 || skipped != 1 {
		t.Fatalf("envelopes=%d skipped=%d err=%v, want 1/1/nil", len(envelopes), skipped, err)
	}
}

// TestListOSPackageAdvisoryFactEnvelopesReadsNothingWithoutKeys pins that empty
// ecosystems or package ids read nothing and open no snapshot.
func TestListOSPackageAdvisoryFactEnvelopesReadsNothingWithoutKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ ecosystems, ids []string }{
		{nil, []string{"pkg:deb/debian/pkg"}},
		{[]string{"debian"}, nil},
		{[]string{" "}, []string{""}},
	} {
		database := &fakeExecQueryer{}
		envelopes, skipped, truncated, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
			context.Background(), tc.ecosystems, tc.ids, 100,
		)
		if err != nil || len(envelopes) != 0 || skipped != 0 || truncated || database.beginReadOnlyRepeatableReadCalls != 0 {
			t.Fatalf("ecosystems=%v ids=%v: got (%d, %d, %v, %v) with %d snapshots, want an empty no-op",
				tc.ecosystems, tc.ids, len(envelopes), skipped, truncated, err, database.beginReadOnlyRepeatableReadCalls)
		}
	}
}

// TestListOSPackageAdvisoryFactEnvelopesRejectsAFullPageThatDoesNotAdvance pins
// the loop's termination guard: a full page whose last fact id equals the
// cursor would repeat forever, so it is an error.
func TestListOSPackageAdvisoryFactEnvelopesRejectsAFullPageThatDoesNotAdvance(t *testing.T) {
	t.Parallel()

	stuck := make([][]any, 0, osPackageAdvisoryDrainPageSize)
	for range osPackageAdvisoryDrainPageSize {
		stuck = append(stuck, osPackageCandidateRow("fact-stuck", true, true))
	}
	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: stuck},
		{rows: append([][]any(nil), stuck...)},
	}}
	_, _, _, err := NewFactStore(database).ListOSPackageAdvisoryFactEnvelopes(
		context.Background(), []string{"debian"}, []string{"pkg:deb/debian/pkg"}, 1_000_000,
	)
	if err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("error = %v, want the did-not-advance guard", err)
	}
}
