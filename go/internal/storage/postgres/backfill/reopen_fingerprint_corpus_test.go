// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"errors"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// errCorpusReopenTestIteration is the sticky backend failure the error
// propagation tests replay.
var errCorpusReopenTestIteration = errors.New("corpus reopen test iteration failure")

// Corpus reopen fingerprint builders (#10877): the workload materialization
// and service catalog correlation changed-evidence gates join the
// deployable_unit_correlation gate, so a late-arriving foreign scope
// restarts their succeeded items instead of leaving partial truth standing.
// These tests pin the claim-fenced record shape, the per-table listings,
// and the scan verdicts hermetically: the reopen UPDATE only touches rows
// the listing flags, and the record only lands under the exact claim.

// corpusReopenCases pairs every allowlisted fingerprint table with the
// domain that writes it.
var corpusReopenCases = []struct {
	table        string
	domain       string
	logicVersion int
}{
	{"deployable_unit_reopen_fingerprints", "deployable_unit_correlation", reducercontract.DeployableUnitCorrelationLogicVersion},
	{"workload_materialization_reopen_fingerprints", "workload_materialization", reducercontract.WorkloadMaterializationLogicVersion},
	{"service_catalog_correlation_reopen_fingerprints", "service_catalog_correlation", reducercontract.ServiceCatalogCorrelationLogicVersion},
}

// TestRecordCorpusReopenFingerprintQueryFencesOnExactClaim pins the record
// shape every gated domain shares: the claimant CTE rechecks the exact
// claim (work item, last_attempt_at, live claim_until) under FOR NO KEY
// UPDATE, and the upsert keeps the newest claim.
func TestRecordCorpusReopenFingerprintQueryFencesOnExactClaim(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusReopenCases {
		query := RecordCorpusReopenFingerprintQuery(tc.domain, tc.table, tc.logicVersion)
		for _, fragment := range []string{
			"FOR NO KEY UPDATE",
			"last_attempt_at = $2",
			"claim_until > clock_timestamp()",
			"INSERT INTO " + tc.table,
			"domain = '" + tc.domain + "'",
			"ON CONFLICT (work_item_id) DO UPDATE",
			tc.table + ".claimed_at <= EXCLUDED.claimed_at",
		} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("record query for %s missing %q:\n%s", tc.table, fragment, query)
			}
		}
	}
}

// TestRecordCorpusReopenFingerprintQueryRendersPerTable proves the three
// domains do not share one rendering: each names its own table and domain.
func TestRecordCorpusReopenFingerprintQueryRendersPerTable(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, tc := range corpusReopenCases {
		query := RecordCorpusReopenFingerprintQuery(tc.domain, tc.table, tc.logicVersion)
		if seen[query] {
			t.Fatalf("record query for %s is byte-identical to another domain's", tc.table)
		}
		seen[query] = true
		for _, other := range corpusReopenCases {
			if other.table == tc.table {
				continue
			}
			if strings.Contains(query, "INSERT INTO "+other.table) ||
				strings.Contains(query, "domain = '"+other.domain+"'") {
				t.Fatalf("record query for %s leaks %s/%s", tc.table, other.domain, other.table)
			}
		}
	}
}

// TestCorpusReopenQueryBuildersRejectUnknownTable pins the allowlist panic:
// a typo fails at initialization instead of querying a wrong table.
func TestCorpusReopenQueryBuildersRejectUnknownTable(t *testing.T) {
	t.Parallel()

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s with an unknown table did not panic", name)
			}
		}()
		fn()
	}

	mustPanic("corpusReopenFingerprintJoin", func() { _ = corpusReopenFingerprintJoin("no_such_table") })
	mustPanic("ListCorpusReopenCandidatesQuery", func() { _ = ListCorpusReopenCandidatesQuery("no_such_table", 1) })
	mustPanic("RecordCorpusReopenFingerprintQuery", func() {
		_ = RecordCorpusReopenFingerprintQuery("workload_materialization", "no_such_table", 1)
	})
	mustPanic("InvalidateCorpusReopenFingerprintsForReposQuery", func() {
		_ = InvalidateCorpusReopenFingerprintsForReposQuery("no_such_table")
	})
	mustPanic("RecordCorpusReopenFingerprintQuery empty domain", func() {
		_ = RecordCorpusReopenFingerprintQuery("  ", "workload_materialization_reopen_fingerprints", 1)
	})
}

// TestInvalidateCorpusReopenFingerprintsForReposQueryDeletesOverlapBatch
// pins the batch invalidation predicate: one statement for every repository
// set a sweep cycle deleted, matched by array overlap.
func TestInvalidateCorpusReopenFingerprintsForReposQueryDeletesOverlapBatch(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusReopenCases {
		query := InvalidateCorpusReopenFingerprintsForReposQuery(tc.table)
		for _, fragment := range []string{
			"DELETE FROM " + tc.table,
			"repo_ids && $1",
		} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("invalidate query for %s missing %q:\n%s", tc.table, fragment, query)
			}
		}
	}
}

// TestListCorpusReopenCandidatesQueryGatesOnChangedInput pins the listing
// shape: the same replay floor as the deployable_unit_correlation listing,
// plus the input_changed verdict over the recorded repository set.
func TestListCorpusReopenCandidatesQueryGatesOnChangedInput(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusReopenCases {
		query := ListCorpusReopenCandidatesQuery(tc.table, tc.logicVersion)
		for _, fragment := range []string{
			"LEFT JOIN " + tc.table,
			"input_changed",
			"work.domain = $1",
		} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("candidates query for %s missing %q:\n%s", tc.table, fragment, query)
			}
		}
	}
}

// TestListCorpusReopenCandidatesQueriesCoversGatedDomains proves the gate
// map leaves no gated domain on unconditional reopen: workload
// materialization and service catalog correlation join
// deployable_unit_correlation behind the changed-evidence gate.
func TestListCorpusReopenCandidatesQueriesCoversGatedDomains(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusReopenCases {
		query, ok := ListCorpusReopenCandidatesQueries[tc.domain]
		if !ok {
			t.Fatalf("gated listing map has no entry for %s", tc.domain)
		}
		if !strings.Contains(query, tc.table) {
			t.Fatalf("gated listing for %s does not read %s", tc.domain, tc.table)
		}
	}
}

// errRows replays staged rows with a sticky iteration error, so scan tests
// prove error propagation instead of only the happy path.
type errRows struct {
	*queueFakeRows
	err error
}

func (r *errRows) Err() error { return r.err }

// TestScanDeployableUnitReopenCandidatesSplitsChangedFromUnchanged proves
// the listing verdict mapping: changed inputs reopen in listing order,
// unchanged inputs count as skipped, and blank ids never reopen.
func TestScanDeployableUnitReopenCandidatesSplitsChangedFromUnchanged(t *testing.T) {
	t.Parallel()

	rows := &queueFakeRows{rows: [][]any{
		{"work-1", true},
		{"work-2", false},
		{"", true},
		{"  ", true},
		{"work-3", true},
	}}
	reopen, skipped, err := ScanDeployableUnitReopenCandidates(rows)
	if err != nil {
		t.Fatalf("scan error = %v, want nil", err)
	}
	if len(reopen) != 2 || reopen[0] != "work-1" || reopen[1] != "work-3" {
		t.Fatalf("reopen = %v, want [work-1 work-3] in listing order", reopen)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (only work-2 is unchanged; blanks never count)", skipped)
	}
}

// TestScanDeployableUnitReopenCandidatesReportsScanError proves a corrupt
// row fails the listing instead of reopening the wrong set.
func TestScanDeployableUnitReopenCandidatesReportsScanError(t *testing.T) {
	t.Parallel()

	rows := &queueFakeRows{rows: [][]any{{"work-1", "not-a-bool"}}}
	if _, _, err := ScanDeployableUnitReopenCandidates(rows); err == nil {
		t.Fatal("scan error = nil, want the corrupt verdict to fail")
	}
}

// TestScanDeployableUnitReopenCandidatesReportsIterationError proves a
// mid-iteration backend failure fails the listing instead of reporting a
// partial set as complete.
func TestScanDeployableUnitReopenCandidatesReportsIterationError(t *testing.T) {
	t.Parallel()

	rows := &errRows{
		queueFakeRows: &queueFakeRows{rows: [][]any{{"work-1", true}}},
		err:           errCorpusReopenTestIteration,
	}
	if _, _, err := ScanDeployableUnitReopenCandidates(rows); err == nil {
		t.Fatal("scan error = nil, want the iteration failure to fail")
	}
}

// TestScanDeployableUnitAdmittedAnchorReposSkipsBlanks proves the admitted
// set carries listing order with blank ids dropped.
func TestScanDeployableUnitAdmittedAnchorReposSkipsBlanks(t *testing.T) {
	t.Parallel()

	rows := &queueFakeRows{rows: [][]any{{"repo-b"}, {""}, {"  "}, {"repo-a"}}}
	anchors, err := ScanDeployableUnitAdmittedAnchorRepos(rows)
	if err != nil {
		t.Fatalf("scan error = %v, want nil", err)
	}
	if len(anchors) != 2 || anchors[0] != "repo-b" || anchors[1] != "repo-a" {
		t.Fatalf("anchors = %v, want [repo-b repo-a] in listing order", anchors)
	}
}

// TestScanDeployableUnitAdmittedAnchorReposReportsErrors proves corrupt
// rows and iteration failures fail the listing.
func TestScanDeployableUnitAdmittedAnchorReposReportsErrors(t *testing.T) {
	t.Parallel()

	if _, err := ScanDeployableUnitAdmittedAnchorRepos(
		&queueFakeRows{rows: [][]any{{int64(7)}}}); err == nil {
		t.Fatal("scan error = nil, want the corrupt anchor to fail")
	}
	if _, err := ScanDeployableUnitAdmittedAnchorRepos(&errRows{
		queueFakeRows: &queueFakeRows{rows: [][]any{{"repo-a"}}},
		err:           errCorpusReopenTestIteration,
	}); err == nil {
		t.Fatal("scan error = nil, want the iteration failure to fail")
	}
}
