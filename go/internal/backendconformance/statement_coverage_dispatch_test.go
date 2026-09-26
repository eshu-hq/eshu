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

// A uid-anchored per-label read (#7089) renders the id predicate as
// `n.uid = $p AND n.id = $p` on labels whose uid is uniquely indexed and as
// `n.id = $p` on the rest. The uid seek only narrows the same id predicate,
// so both spellings are one logical read. These tests pin that the family
// key folds the seek conjunct, and only that exact conjunct, so one
// exemption keyed by the id-only text covers the whole family and no
// exemption list has to grow a second entry per spelling.
const (
	uidSeekLabelA    = "MATCH (n:Alpha) WHERE n.uid = $entity_id AND n.id = $entity_id RETURN n.id AS id"
	uidSeekLabelB    = "MATCH (n:Beta) WHERE n.uid = $entity_id AND n.id = $entity_id RETURN n.id AS id"
	uidSeekIDOnly    = "MATCH (n:Gamma) WHERE n.id = $entity_id RETURN n.id AS id"
	uidSeekIDFamily  = dispatchUnlabeled
	uidSeekBothParam = `{"entity_id":"id-1","other":"id-1"}`
)

// TestStatementCoverageUIDSeekConjunctJoinsTheIDAnchorFamily: uid-and-id
// members and an id-only member with the same parameters are one family
// keyed by the id-only unlabeled text, so the one exemption naming that
// text excuses all of them.
func TestStatementCoverageUIDSeekConjunctJoinsTheIDAnchorFamily(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(uidSeekLabelA, dispatchParams, 0),
		dispatchRead(uidSeekLabelB, dispatchParams, 0),
		dispatchRead(uidSeekIDOnly, dispatchParams, 0),
		dispatchRead(dispatchUnlabeled, dispatchParams, 0),
	}}
	manifest := dispatchManifest(queryplan.ReadExemption{
		Statement: uidSeekIDFamily,
		Reason:    "probe-by-id over an absent entity",
	})
	failures := ComputeStatementCoverage(manifest, records).Failures()
	if len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: the id-only exemption covers the folded family", failures)
	}
}

// TestStatementCoverageUIDSeekFamilyFailsOnceUnderTheIDKey: without an
// exemption the joined family is one always-empty read named by the
// id-only key, not one per spelling.
func TestStatementCoverageUIDSeekFamilyFailsOnceUnderTheIDKey(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(uidSeekLabelA, dispatchParams, 0),
		dispatchRead(uidSeekIDOnly, dispatchParams, 0),
	}}
	failures := ComputeStatementCoverage(dispatchManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, uidSeekIDFamily)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want exactly one family failure", failures)
	}
}

// TestStatementCoverageUIDSeekDifferentParameterStaysItsOwnRead is a
// narrowness guard: `n.uid = $entity_id AND n.id = $other` does not imply
// `n.id = $entity_id`, so it must not fold into the id-only family and be
// silently excused by that family's exemption.
func TestStatementCoverageUIDSeekDifferentParameterStaysItsOwnRead(t *testing.T) {
	mismatchA := "MATCH (n:Alpha) WHERE n.uid = $entity_id AND n.id = $other RETURN n.id AS id"
	mismatchB := "MATCH (n:Beta) WHERE n.uid = $entity_id AND n.id = $other RETURN n.id AS id"
	foldedWrongly := "MATCH (n) WHERE n.id = $other RETURN n.id AS id"
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(mismatchA, uidSeekBothParam, 0),
		dispatchRead(mismatchB, uidSeekBothParam, 0),
	}}
	manifest := dispatchManifest(queryplan.ReadExemption{Statement: foldedWrongly, Reason: "wrong fold"})
	failures := ComputeStatementCoverage(manifest, records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead,
		"MATCH (n) WHERE n.uid = $entity_id AND n.id = $other RETURN n.id AS id")
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the unfolded mismatched-parameter read", failures)
	}
}

// TestStatementCoverageUIDSeekDifferentVariableStaysItsOwnRead is a
// narrowness guard: `m.uid = $p AND n.id = $p` constrains two variables, so
// it is not the seek conjunct for one node and must not fold.
func TestStatementCoverageUIDSeekDifferentVariableStaysItsOwnRead(t *testing.T) {
	mismatchA := "MATCH (n:Alpha) WHERE m.uid = $entity_id AND n.id = $entity_id RETURN n.id AS id"
	mismatchB := "MATCH (n:Beta) WHERE m.uid = $entity_id AND n.id = $entity_id RETURN n.id AS id"
	records := map[string][]DifferentialRecord{"neo4j": {
		dispatchRead(mismatchA, dispatchParams, 0),
		dispatchRead(mismatchB, dispatchParams, 0),
	}}
	manifest := dispatchManifest(queryplan.ReadExemption{
		Statement: "MATCH (n) WHERE n.id = $entity_id RETURN n.id AS id",
		Reason:    "wrong fold",
	})
	failures := ComputeStatementCoverage(manifest, records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead,
		"MATCH (n) WHERE m.uid = $entity_id AND n.id = $entity_id RETURN n.id AS id")
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the unfolded cross-variable read", failures)
	}
}

// TestStatementCoverageUIDOnlyAndOrFormsStayTheirOwnRead is a narrowness
// guard: a uid-only predicate and an OR form do not imply the id predicate,
// so neither folds into the id-only family.
func TestStatementCoverageUIDOnlyAndOrFormsStayTheirOwnRead(t *testing.T) {
	cases := map[string][2]string{
		"uid-only": {
			"MATCH (n:Alpha) WHERE n.uid = $entity_id RETURN n.id AS id",
			"MATCH (n:Beta) WHERE n.uid = $entity_id RETURN n.id AS id",
		},
		"or-form": {
			"MATCH (n:Alpha) WHERE n.uid = $entity_id OR n.id = $entity_id RETURN n.id AS id",
			"MATCH (n:Beta) WHERE n.uid = $entity_id OR n.id = $entity_id RETURN n.id AS id",
		},
	}
	for name, texts := range cases {
		t.Run(name, func(t *testing.T) {
			records := map[string][]DifferentialRecord{"neo4j": {
				dispatchRead(texts[0], dispatchParams, 0),
				dispatchRead(texts[1], dispatchParams, 0),
			}}
			manifest := dispatchManifest(queryplan.ReadExemption{
				Statement: uidSeekIDFamily,
				Reason:    "id-only family only",
			})
			failures := ComputeStatementCoverage(manifest, records).Failures()
			if len(failures) != 1 {
				t.Fatalf("Failures() = %v, want one unfolded always-empty read", failures)
			}
			if failures[0].Kind != CoverageAlwaysEmptyRead {
				t.Fatalf("failure kind = %v, want %v", failures[0].Kind, CoverageAlwaysEmptyRead)
			}
		})
	}
}
