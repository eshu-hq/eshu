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
	} {
		if !strings.Contains(narrowed, want) {
			t.Fatalf("narrowed query missing %q:\n%s", want, narrowed)
		}
	}
	for _, banned := range []string{"ROW_NUMBER", "COUNT(*) OVER", "MOD("} {
		if strings.Contains(narrowed, banned) {
			t.Fatalf("narrowed query contains %q; a window forces a full read and sort per page", banned)
		}
	}
	// The ecosystem predicate is the rotation reader's, so a paged drain and the
	// planner see the same rows.
	ecosystem := "LOWER(COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro')) = ANY($4::text[])"
	if !strings.Contains(narrowed, ecosystem) {
		t.Fatalf("narrowed query lost the ecosystem predicate %q", ecosystem)
	}
	if !strings.Contains(rotation, "LOWER(COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) = ANY($1::text[])") {
		t.Fatal("rotation query changed its ecosystem predicate; update the narrowed query in step")
	}
}

func osPackageCandidateRow(factID string, active, complete bool) []any {
	version := "1.0-1"
	if !complete {
		version = ""
	}
	return []any{
		"debian", "debian", "12", "pkg", version, "dpkg", "amd64", "debian", "vendor", "pkg:deb/debian/pkg",
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
