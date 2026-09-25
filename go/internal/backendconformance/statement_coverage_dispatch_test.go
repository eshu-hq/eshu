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

// An independent per-label fan-out is the second shape the family rule
// groups (fetchOCIImagesByDigest in query/impact/trace_deployment_oci.go
// runs one single-label digest lookup per image label with the same digests
// batch and concatenates every label's rows; the resource-investigation
// selector fan-out in query/impact/resource_investigation_selector.go does
// the same, concurrently, with one params map). Nothing stops at a first
// hit, but the recordings look identical to a dispatch: same text modulo
// the anchor label, byte-identical parameters. The gate groups them by
// design, so an expected non-owning-label miss is advisory rather than an
// always-empty read. These tests pin that shape as intended, not as an
// accident of the dispatch rule.
const (
	fanoutParams     = `{"digests":["sha256:abc"]}`
	fanoutImageIndex = "MATCH (image:ContainerImageIndex) WHERE image.digest IN $digests RETURN image.digest AS digest"
	fanoutImageDesc  = "MATCH (image:ContainerImageDescriptor) WHERE image.digest IN $digests RETURN image.digest AS digest"
	fanoutUnlabeled  = "MATCH (image) WHERE image.digest IN $digests RETURN image.digest AS digest"
)

// TestStatementCoveragePerLabelFanoutWithSharedParamsGroupsWithoutFailure:
// both labels are probed with the same batch and both return rows (the
// concatenate semantics). The family returned rows, so nothing fails and
// nothing is a miss.
func TestStatementCoveragePerLabelFanoutWithSharedParamsGroupsWithoutFailure(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(fanoutImageIndex, fanoutParams, 2),
		dispatchRead(fanoutImageDesc, fanoutParams, 3),
	}}
	report := ComputeStatementCoverage(dispatchManifest(), records)
	if failures := report.Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: every fan-out label returned rows", failures)
	}
	if misses := report.ByBackend["neo4j"].DispatchMisses; len(misses) != 0 {
		t.Fatalf("DispatchMisses = %v, want none", misses)
	}
}

// TestStatementCoveragePerLabelFanoutMissIsAdvisoryWhileSiblingHits: the
// digest lives on only one label, so the other label's probe misses by
// construction. The fan-out is judged once: no failure, and the miss is
// reported as advisory. This is also the disclosed masking trade-off: a
// fan-out member broken on both backends stays green while a sibling
// returns rows, and only the advisory line shows it.
func TestStatementCoveragePerLabelFanoutMissIsAdvisoryWhileSiblingHits(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(fanoutImageIndex, fanoutParams, 0),
		dispatchRead(fanoutImageDesc, fanoutParams, 3),
	}}
	report := ComputeStatementCoverage(dispatchManifest(), records)
	if failures := report.Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: the fan-out returned rows via a sibling label", failures)
	}
	misses := report.ByBackend["neo4j"].DispatchMisses
	if len(misses) != 1 || misses[0] != fanoutImageIndex {
		t.Fatalf("DispatchMisses = %v, want only %q", misses, fanoutImageIndex)
	}
}

// TestStatementCoveragePerLabelFanoutAllMissFailsOnceWithoutExemption: when
// no label of the fan-out ever returns rows, the grouped read is still an
// always-empty read, reported once under its unlabeled text.
func TestStatementCoveragePerLabelFanoutAllMissFailsOnceWithoutExemption(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(fanoutImageIndex, fanoutParams, 0),
		dispatchRead(fanoutImageDesc, fanoutParams, 0),
	}}
	failures := ComputeStatementCoverage(dispatchManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, fanoutUnlabeled)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want exactly one fan-out family failure", failures)
	}
}
