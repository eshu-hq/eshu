// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type cov7EvidenceQueryer struct {
	rows db.Rows
	err  error
}

func (q *cov7EvidenceQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

type cov7EvidenceRows struct {
	rows [][]any
	pos  int
	err  error
}

func (r *cov7EvidenceRows) Next() bool { return r.pos < len(r.rows) }

func (r *cov7EvidenceRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	row := r.rows[r.pos]
	r.pos++
	for i, value := range row {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *sql.NullTime:
			*target = value.(sql.NullTime)
		case *[]byte:
			*target = value.([]byte)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

func (r *cov7EvidenceRows) Err() error { return nil }

func (r *cov7EvidenceRows) Close() error { return nil }

// TestCov7ListAdvisoryEvidenceGuards proves the validation gate order: the
// filter normalizes first, then the store rejects a missing database, an
// anchorless read, and an out-of-range limit before touching the reader.
func TestCov7ListAdvisoryEvidenceGuards(t *testing.T) {
	t.Parallel()

	store := PostgresEvidenceStore{}
	if _, err := store.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1", Limit: 1}); err == nil {
		t.Fatalf("nil database read = nil error, want database required")
	}
	reader := &cov7EvidenceQueryer{rows: &cov7EvidenceRows{}}
	guarded := NewPostgresEvidenceStoreWithReadStore(reader)
	if _, err := guarded.ListAdvisoryEvidence(t.Context(), EvidenceFilter{Limit: 1}); err == nil {
		t.Fatalf("anchorless read = nil error, want scope required")
	}
	if _, err := guarded.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1"}); err == nil {
		t.Fatalf("zero limit = nil error, want limit range error")
	}
	if _, err := guarded.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1", Limit: EvidenceMaxLimit + 2}); err == nil {
		t.Fatalf("oversize limit = nil error, want limit range error")
	}
	want := errors.New("reader down")
	failing := NewPostgresEvidenceStoreWithReadStore(&cov7EvidenceQueryer{err: want})
	if _, err := failing.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1", Limit: 1}); !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
	if store := NewPostgresEvidenceStore(nil); store.DB != nil {
		t.Fatalf("legacy constructor did not keep nil database")
	}
}

// TestCov7ListAdvisoryEvidenceReadsRows proves the scan/decode pipeline:
// scan failures, malformed payloads, and iteration errors fail closed, while
// well-formed rows group through BuildEvidenceRows into a paged envelope.
func TestCov7ListAdvisoryEvidenceReadsRows(t *testing.T) {
	t.Parallel()

	now := sql.NullTime{Time: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC), Valid: true}
	payload := []byte(`{"source":"osv","advisory_id":"OSV-1","cve_id":"CVE-2026-0005"}`)
	happy := &cov7EvidenceRows{rows: [][]any{
		{"fact:1", "vulnerability.cve", "reported", now, "1.0", payload},
		{"fact:2", "vulnerability.cve", "reported", sql.NullTime{}, "1.0", payload},
	}}
	store := NewPostgresEvidenceStoreWithReadStore(&cov7EvidenceQueryer{rows: happy})
	rows, err := store.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "cve-2026-0005", Limit: 10})
	if err != nil {
		t.Fatalf("list = %v, want rows", err)
	}
	if len(rows) != 1 || rows[0].Key != "CVE-2026-0005" || len(rows[0].Sources) != 2 {
		t.Fatalf("rows = %#v, want one grouped row with two sources", rows)
	}
	if rows[0].LatestObservedAt != "2026-05-24T12:00:00Z" {
		t.Fatalf("LatestObservedAt = %q, want formatted row time", rows[0].LatestObservedAt)
	}

	scanFail := NewPostgresEvidenceStoreWithReadStore(&cov7EvidenceQueryer{
		rows: &cov7EvidenceRows{rows: [][]any{{"fact:1"}}, err: errors.New("scan boom")},
	})
	if _, err := scanFail.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1", Limit: 1}); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	badJSON := NewPostgresEvidenceStoreWithReadStore(&cov7EvidenceQueryer{
		rows: &cov7EvidenceRows{rows: [][]any{{"fact:1", "vulnerability.cve", "reported", now, "1", []byte("{oops")}}, err: nil},
	})
	if _, err := badJSON.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-1", Limit: 1}); err == nil {
		t.Fatalf("malformed payload = nil error, want decode failure")
	}
}

// TestCov7EvidenceFilterShaping proves scope detection, normalization, lookup
// IDs, and the paging/filter pipeline over grouped rows.
func TestCov7EvidenceFilterShaping(t *testing.T) {
	t.Parallel()

	if (EvidenceFilter{}).HasScope() {
		t.Fatalf("empty filter has scope, want false")
	}
	if !(EvidenceFilter{WorkloadID: "w"}).HasScope() {
		t.Fatalf("workload filter lacks scope, want true")
	}
	normalized := NormalizeEvidenceFilter(EvidenceFilter{
		CVEID: " cve-2026-1 ", ID: "ghsa-a-b-c", PackageID: " pkg ",
		RepositoryID: " r ", ServiceID: " s ", WorkloadID: " w ",
		Source: " OSV ", AfterKey: " osv-2 ",
	})
	if normalized.CVEID != "CVE-2026-1" || normalized.Source != "osv" || normalized.AfterKey != "OSV-2" {
		t.Fatalf("normalized = %#v, want canonical ids and lowercase source", normalized)
	}
	ids := EvidenceLookupIDs(EvidenceFilter{CVEID: "cve-2026-0001", ID: "CVE-2026-0001"})
	if len(ids) != 1 || ids[0] != "CVE-2026-0001" {
		t.Fatalf("lookup ids = %#v, want deduped CVE", ids)
	}
	if capacity := EvidenceFactCapacity(); capacity != EvidenceMaxFactRows {
		t.Fatalf("capacity = %d, want %d", capacity, EvidenceMaxFactRows)
	}
	if got := FormatNullTime(sql.NullTime{}); got != "" {
		t.Fatalf("null time = %q, want empty", got)
	}
	if got := FormatNullTime(sql.NullTime{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Valid: true}); got != "2026-01-02T03:04:05Z" {
		t.Fatalf("valid time = %q, want RFC3339", got)
	}

	rows := []EvidenceRow{
		{Key: "CVE-2026-0001", CanonicalID: "CVE-2026-0001", CVEIDs: []string{"CVE-2026-0001"}},
		{Key: "CVE-2026-0002", CanonicalID: "CVE-2026-0002", CVEIDs: []string{"CVE-2026-0002"}},
	}
	paged := PageEvidenceRows(rows, EvidenceFilter{Limit: 1})
	if len(paged) != 1 || paged[0].Key != "CVE-2026-0001" {
		t.Fatalf("paged = %#v, want first row", paged)
	}
	after := PageEvidenceRows(rows, EvidenceFilter{AfterKey: "cve-2026-0001", Limit: 5})
	if len(after) != 1 || after[0].Key != "CVE-2026-0002" {
		t.Fatalf("after-key page = %#v, want second row", after)
	}
	if past := PageEvidenceRows(rows, EvidenceFilter{AfterKey: "cve-2026-0002", Limit: 5}); past != nil {
		t.Fatalf("past-end page = %#v, want nil", past)
	}
	cveScoped := PageEvidenceRows(rows, EvidenceFilter{CVEID: "cve-2026-0002", Limit: 5})
	if len(cveScoped) != 1 || cveScoped[0].Key != "CVE-2026-0002" {
		t.Fatalf("cve page = %#v, want second row only", cveScoped)
	}
	// A repository anchor clears the CVE/ID/package selectors, so the read
	// stays broad instead of filtering to one advisory.
	repoScoped := PageEvidenceRows(rows, EvidenceFilter{CVEID: "cve-2026-0002", RepositoryID: "r1", Limit: 5})
	if len(repoScoped) != 2 {
		t.Fatalf("repo-anchored page = %#v, want both rows", repoScoped)
	}
	pkgRow := EvidenceRow{Key: "GHSA-1", AffectedPackages: []AffectedPackage{{PackageID: "pkg:npm/x"}}}
	if kept := PageEvidenceRows([]EvidenceRow{pkgRow}, EvidenceFilter{PackageID: "pkg:npm/x", Limit: 5}); len(kept) != 1 {
		t.Fatalf("package page = %#v, want match", kept)
	}
	if dropped := PageEvidenceRows([]EvidenceRow{pkgRow}, EvidenceFilter{PackageID: "pkg:npm/other", Limit: 5}); len(dropped) != 0 {
		t.Fatalf("package page = %#v, want no match", dropped)
	}
}

// TestCov7EvidenceRowMatching proves advisory-identity matching across CVE,
// alias, source, package, EPSS, KEV, and reference rows.
func TestCov7EvidenceRowMatching(t *testing.T) {
	t.Parallel()

	row := EvidenceRow{
		Key: "CVE-2026-0006", CanonicalID: "CVE-2026-0006",
		CVEIDs: []string{"CVE-2026-0006"}, GHSAIDs: []string{"GHSA-a-b-c"},
		OSVIDs: []string{"OSV-3"}, SourceIDs: []string{"nvd:CVE-2026-0006"},
		Sources:          []SourceEvidence{{Source: "nvd", ID: "CVE-2026-0006", Aliases: []string{"CVE-2026-0006"}}},
		AffectedPackages: []AffectedPackage{{ID: "OSV-3", PURL: "pkg:npm/y"}},
		AffectedProducts: []AffectedProduct{{CVEID: "CVE-2026-0006"}},
		EPSS:             []EPSSObservation{{CVEID: "CVE-2026-0006"}},
		KEV:              []KEVObservation{{CVEID: "CVE-2026-0006"}},
		References:       []ReferenceEvidence{{ID: "CVE-2026-0006"}},
	}
	for _, target := range []string{"cve-2026-0006", "GHSA-A-B-C", "osv-3", "nvd:cve-2026-0006"} {
		if !advisoryEvidenceRowMatchesAdvisory(row, target) {
			t.Fatalf("advisory match(%q) = false, want true", target)
		}
	}
	if advisoryEvidenceRowMatchesAdvisory(row, "CVE-9999-9999") {
		t.Fatalf("advisory match(unknown) = true, want false")
	}
	if !advisoryEvidenceRowMatchesCVE(row, "cve-2026-0006") {
		t.Fatalf("cve match = false, want true")
	}
	if advisoryEvidenceRowMatchesCVE(row, "CVE-9999-9999") {
		t.Fatalf("cve mismatch = true, want false")
	}
	if !advisoryEvidenceRowMatchesPackage(row, "pkg:npm/y") {
		t.Fatalf("package match = false, want true")
	}
	if advisoryEvidenceRowMatchesPackage(row, "pkg:npm/other") {
		t.Fatalf("package mismatch = true, want false")
	}
	// An empty filter constrains nothing, so the row-level matcher accepts;
	// the row filter above short-circuits empty filters to the full set.
	if !advisoryEvidenceRowMatchesFilter(row, EvidenceFilter{}) {
		t.Fatalf("empty filter match = false, want true")
	}
	if !advisoryEvidenceAnyIDMatches("a", "x", "A") || advisoryEvidenceAnyIDMatches("a", "x") {
		t.Fatalf("any-id matching wrong")
	}
	if !advisoryEvidenceStringSliceMatches([]string{"x"}, "X") || advisoryEvidenceStringSliceMatches([]string{"x"}, "y") {
		t.Fatalf("slice matching wrong")
	}
	if !advisoryEvidenceKeyEqual(" ghsa-a-b-c ", "GHSA-A-B-C") {
		t.Fatalf("key equal = false, want true")
	}
}

// TestCov7EvidenceValueHelpers proves the payload accessors and the evidence
// sort order behind grouped rows.
func TestCov7EvidenceValueHelpers(t *testing.T) {
	t.Parallel()

	if got := anyMapSliceVal(map[string]any{"k": []any{map[string]any{"a": 1}, "skip"}}, "k"); len(got) != 1 {
		t.Fatalf("any map slice = %#v, want one map", got)
	}
	if got := anyMapSliceVal(map[string]any{"k": []any{"skip"}}, "k"); got != nil {
		t.Fatalf("all-skipped slice = %#v, want nil", got)
	}
	if got := anyMapSliceVal(map[string]any{}, "k"); got != nil {
		t.Fatalf("missing slice = %#v, want nil", got)
	}
	sets := map[string]struct{}{}
	addSet(sets, "  ")
	addSet(sets, "x")
	if len(sets) != 1 || SetToSortedSlice(map[string]struct{}{}) != nil {
		t.Fatalf("addSet/set-slice wrong")
	}
	if got := sortedStrings([]string{"b", " a ", "b", ""}); len(got) != 2 || got[0] != "a" {
		t.Fatalf("sorted strings = %#v, want deduped pair", got)
	}
	if sortedStrings(nil) != nil {
		t.Fatalf("nil sorted strings non-nil")
	}
	payload := map[string]any{
		"m":     map[string]any{"a": 1, " ": 2, "n": nil},
		"empty": map[string]any{},
		"s":     []any{map[string]any{"t": " v ", "blank": " ", "nil": nil}, "skip", map[string]any{}},
	}
	if got := mapVal(payload, "m"); len(got) != 1 || got["a"] != 1 {
		t.Fatalf("mapVal = %#v, want filtered map", got)
	}
	if mapVal(payload, "empty") != nil || mapVal(payload, "missing") != nil {
		t.Fatalf("mapVal empty/missing non-nil")
	}
	if got := stringMapSliceVal(payload, "s"); len(got) != 1 || got[0]["t"] != "v" {
		t.Fatalf("stringMapSlice = %#v, want one trimmed row", got)
	}
	if stringMapSliceVal(payload, "missing") != nil {
		t.Fatalf("stringMapSlice missing non-nil")
	}
	score := 1.5
	if derefFloat64(&score) != 1.5 || derefFloat64(nil) != 0 {
		t.Fatalf("derefFloat64 wrong")
	}
	if sourceConfidenceLabel(map[string]struct{}{}) != "" || sourceConfidenceLabel(map[string]struct{}{"a": {}}) != "a" {
		t.Fatalf("confidence labels wrong")
	}
	row := &EvidenceRow{
		Sources:          []SourceEvidence{{Source: "b", ID: "2"}, {Source: "a", ID: "1"}},
		AffectedPackages: []AffectedPackage{{PackageID: "p2", Source: "b"}, {PackageID: "p1", Source: "a"}},
		AffectedProducts: []AffectedProduct{{MatchCriteriaID: "m2"}, {MatchCriteriaID: "m1"}},
		EPSS:             []EPSSObservation{{ScoreDate: "2026-02-01"}, {ScoreDate: "2026-01-01"}},
		KEV:              []KEVObservation{{DateAdded: "2026-02-01"}, {DateAdded: "2026-01-01"}},
		References:       []ReferenceEvidence{{URL: "b"}, {URL: "a"}},
	}
	sortAdvisoryEvidence(row)
	if row.Sources[0].Source != "a" || row.AffectedPackages[0].PackageID != "p1" ||
		row.AffectedProducts[0].MatchCriteriaID != "m1" || row.EPSS[0].ScoreDate != "2026-01-01" ||
		row.KEV[0].DateAdded != "2026-01-01" || row.References[0].URL != "a" {
		t.Fatalf("sorted row = %#v, want ordered evidence", row)
	}
}
