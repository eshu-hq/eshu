// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// A label-dispatch read (issue #7006) resolves one id by trying one
// single-label MATCH per candidate label until one returns a row: on the
// pinned NornicDB build a label disjunction silently returns zero rows, so
// one logical read is split into N statement texts that differ only in the
// leading anchor label. For any one id, every label ordered before the
// owning label misses by construction. These tests pin that the coverage
// gate judges such a family as the one read it is, and only when the
// recordings themselves prove the dispatch (the same parameters probed
// against sibling labels).
const (
	dispatchParams      = `{"entity_id":"id-1"}`
	dispatchLabelA      = "MATCH (n:Alpha) WHERE n.id = $entity_id RETURN n.id AS id"
	dispatchLabelB      = "MATCH (n:Beta) WHERE n.id = $entity_id RETURN n.id AS id"
	dispatchUnlabeled   = "MATCH (n) WHERE n.id = $entity_id RETURN n.id AS id"
	dispatchOtherParams = `{"entity_id":"id-2"}`
)

func dispatchRead(statement, params string, rows int) DifferentialRecord {
	digest := "empty-digest"
	if rows > 0 {
		digest = "rows-digest"
	}
	return DifferentialRecord{
		Backend:     "neo4j",
		Fingerprint: DifferentialFingerprint{Statement: statement, Parameters: params},
		RowCount:    rows,
		Digest:      digest,
	}
}

func dispatchManifest(exemptions ...queryplan.ReadExemption) queryplan.BuilderManifest {
	return queryplan.BuilderManifest{Version: 1, ReadExemptions: exemptions}
}

// TestStatementCoverageLabelDispatchMissBeforeHitIsNotAlwaysEmpty: the
// Alpha probe missed only because the id lives on Beta, which the same
// parameters resolved. The family returned rows, so Alpha is not an
// always-empty read.
func TestStatementCoverageLabelDispatchMissBeforeHitIsNotAlwaysEmpty(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(dispatchLabelA, dispatchParams, 0),
		dispatchRead(dispatchLabelB, dispatchParams, 1),
	}}
	failures := ComputeStatementCoverage(dispatchManifest(), records).Failures()
	if len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: the dispatch family returned rows", failures)
	}
}

// TestStatementCoverageAllMissDispatchFamilyUsesTheUnlabeledExemption: a
// probe over an absent id misses on every label and on the unlabeled
// fallback. The family is one read, keyed by its unlabeled text, so the
// exemption that names the unlabeled read excuses every label member.
func TestStatementCoverageAllMissDispatchFamilyUsesTheUnlabeledExemption(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(dispatchLabelA, dispatchParams, 0),
		dispatchRead(dispatchLabelB, dispatchParams, 0),
		dispatchRead(dispatchUnlabeled, dispatchParams, 0),
	}}
	manifest := dispatchManifest(queryplan.ReadExemption{
		Statement: dispatchUnlabeled,
		Reason:    "probe-by-id over an absent entity",
	})
	failures := ComputeStatementCoverage(manifest, records).Failures()
	if len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: the unlabeled exemption covers the family", failures)
	}
}

// TestStatementCoverageAllMissDispatchFamilyFailsOnceWithoutExemption: an
// unexempted all-miss family is still an always-empty read, reported once
// under its unlabeled text rather than once per label.
func TestStatementCoverageAllMissDispatchFamilyFailsOnceWithoutExemption(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(dispatchLabelA, dispatchParams, 0),
		dispatchRead(dispatchLabelB, dispatchParams, 0),
	}}
	failures := ComputeStatementCoverage(dispatchManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, dispatchUnlabeled)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want exactly one family failure", failures)
	}
}

// TestStatementCoverageSiblingLabelsWithoutSharedParametersStayIndependent
// is the narrowness guard: two reads that differ only in their anchor
// label but were never executed with the same parameters carry no
// dispatch evidence, so an always-empty one still fails on its own text.
func TestStatementCoverageSiblingLabelsWithoutSharedParametersStayIndependent(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(dispatchLabelA, dispatchParams, 0),
		dispatchRead(dispatchLabelB, dispatchOtherParams, 1),
	}}
	failures := ComputeStatementCoverage(dispatchManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, dispatchLabelA)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the independent Alpha read", failures)
	}
}

// TestStatementCoverageReportsDispatchMissesAsAdvisory keeps the excused
// label misses visible: the report names which anchor labels the corpus
// never positively exercised, without failing the gate on them.
func TestStatementCoverageReportsDispatchMissesAsAdvisory(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(dispatchLabelA, dispatchParams, 0),
		dispatchRead(dispatchLabelB, dispatchParams, 1),
	}}
	coverage := ComputeStatementCoverage(dispatchManifest(), records).ByBackend["neo4j"]
	if len(coverage.DispatchMisses) != 1 || coverage.DispatchMisses[0] != dispatchLabelA {
		t.Fatalf("DispatchMisses = %v, want only %q", coverage.DispatchMisses, dispatchLabelA)
	}
	if len(coverage.AlwaysEmptyReads) != 0 {
		t.Fatalf("AlwaysEmptyReads = %v, want none", coverage.AlwaysEmptyReads)
	}
}
