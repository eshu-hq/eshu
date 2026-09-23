// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

func coverageManifestFixture() queryplan.BuilderManifest {
	return queryplan.BuilderManifest{
		Version: 1,
		Builders: []queryplan.StatementBuilderCoverage{
			{
				File: "writer.go",
				Builders: []queryplan.StatementBuilder{
					{
						Symbol:       "buildUpsert",
						Count:        1,
						Operation:    "sourcecypher.OperationCanonicalUpsert",
						Variants:     []queryplan.StatementVariant{{Template: "MERGE (n:File {path: $path})"}},
						SourceDigest: "aaa",
					},
					{
						Symbol:    "buildLabel",
						Count:     1,
						Operation: "sourcecypher.OperationCanonicalUpsert",
						Variants: []queryplan.StatementVariant{{Fragments: []string{
							"MERGE (n:Label) RETURN",
							"n",
						}}},
						SourceDigest: "bbb",
					},
					{
						Symbol:       "buildForwarded",
						Count:        1,
						Operation:    "sourcecypher.OperationCanonicalUpsert",
						Variants:     []queryplan.StatementVariant{{}},
						SourceDigest: "ccc",
						Exempt:       "forwards caller-built statements; text originates at inventoried builders",
					},
				},
			},
		},
		ReadExemptions: []queryplan.ReadExemption{
			{Statement: "MATCH (a:Admin) RETURN a", Reason: "admin-only read, covered by live test TestAdminList"},
		},
	}
}

func coverageRecordsFixture() map[string][]DifferentialRecord {
	return map[string][]DifferentialRecord{
		"nornicdb": {
			{
				Backend:     "nornicdb",
				Fingerprint: DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`},
				RowCount:    0,
			},
			{
				Backend:     "nornicdb",
				Fingerprint: DifferentialFingerprint{Statement: "MATCH (r:Repository) RETURN r", Parameters: `{}`},
				RowCount:    0,
				Digest:      "empty-digest",
			},
			{
				Backend:     "nornicdb",
				Fingerprint: DifferentialFingerprint{Statement: "MATCH (a:Admin) RETURN a", Parameters: `{}`},
				RowCount:    0,
				Digest:      "empty-digest",
			},
		},
		"neo4j": {
			{
				Backend:     "neo4j",
				Fingerprint: DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`},
				RowCount:    0,
			},
		},
	}
}

// TestComputeStatementCoverageFailsSeededViolations is the RED half of the
// gate's seeded pair: an inventoried builder no run executes plus an
// always-empty read fail the report, while the exempt builder and the
// exempt read stay silent.
func TestComputeStatementCoverageFailsSeededViolations(t *testing.T) {
	report := ComputeStatementCoverage(coverageManifestFixture(), coverageRecordsFixture())
	failures := report.Failures()
	assertFailure(t, failures, "nornicdb", CoverageNeverExecuted, "writer.go:buildLabel")
	assertFailure(t, failures, "neo4j", CoverageNeverExecuted, "writer.go:buildLabel")
	assertFailure(t, failures, "nornicdb", CoverageAlwaysEmptyRead, "MATCH (r:Repository) RETURN r")
	assertNoFailureFor(t, failures, "writer.go:buildForwarded")
	assertNoFailureFor(t, failures, "MATCH (a:Admin) RETURN a")
	if len(failures) != 3 {
		t.Fatalf("Failures() = %v, want exactly the 3 seeded violations", failures)
	}
}

// TestComputeStatementCoveragePassesCoveredManifest is the GREEN half: the
// same manifest with every rule satisfied reports no failures.
func TestComputeStatementCoveragePassesCoveredManifest(t *testing.T) {
	records := coverageRecordsFixture()
	records["nornicdb"] = append(records["nornicdb"],
		DifferentialRecord{
			Backend:     "nornicdb",
			Fingerprint: DifferentialFingerprint{Statement: "MERGE (n:Label) RETURN n", Parameters: `{}`},
			RowCount:    0,
		},
		DifferentialRecord{
			Backend:     "nornicdb",
			Fingerprint: DifferentialFingerprint{Statement: "MATCH (r:Repository) RETURN r", Parameters: `{}`},
			RowCount:    3,
			Digest:      "rows-digest",
		},
	)
	records["neo4j"] = append(records["neo4j"],
		DifferentialRecord{
			Backend:     "neo4j",
			Fingerprint: DifferentialFingerprint{Statement: "MERGE (n:Label) RETURN n", Parameters: `{}`},
			RowCount:    0,
		},
		DifferentialRecord{
			Backend:     "neo4j",
			Fingerprint: DifferentialFingerprint{Statement: "MATCH (r:Repository) RETURN r", Parameters: `{}`},
			RowCount:    2,
			Digest:      "rows-digest",
		},
	)
	report := ComputeStatementCoverage(coverageManifestFixture(), records)
	if failures := report.Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none", failures)
	}
}

func TestComputeStatementCoverageListsUnattributedRecords(t *testing.T) {
	records := map[string][]DifferentialRecord{
		"nornicdb": {
			{
				Backend:     "nornicdb",
				Fingerprint: DifferentialFingerprint{Statement: "MERGE (x:Unknown) RETURN x", Parameters: `{}`},
			},
		},
	}
	report := ComputeStatementCoverage(coverageManifestFixture(), records)
	backend, ok := report.ByBackend["nornicdb"]
	if !ok {
		t.Fatal("report has no nornicdb section")
	}
	found := false
	for _, text := range backend.Unattributed {
		if text == "MERGE (x:Unknown) RETURN x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Unattributed = %v, want the unknown text", backend.Unattributed)
	}
	// Unattributed executions prove activity, not a gap: never a failure.
	assertNoFailureFor(t, report.Failures(), "MERGE (x:Unknown) RETURN x")
}

func assertFailure(t *testing.T, failures []StatementCoverageFailure, backend, kind, id string) {
	t.Helper()
	for _, failure := range failures {
		if failure.Backend == backend && failure.Kind == kind && failure.ID == id {
			return
		}
	}
	t.Fatalf("Failures() = %v, want {%s %s %s}", failures, backend, kind, id)
}

func assertNoFailureFor(t *testing.T, failures []StatementCoverageFailure, id string) {
	t.Helper()
	for _, failure := range failures {
		if failure.ID == id {
			t.Fatalf("Failures() = %v, must not contain %q", failures, id)
		}
	}
}
