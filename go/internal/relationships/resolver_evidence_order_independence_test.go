// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

// TestAggregateCandidateEvidencePreviewIsOrderIndependent is a known-failing
// regression test for the cross-run indexing nondeterminism found while proving
// the #4594 disaster-recovery rebuild. It is expected to fail until the ordering
// fix lands; it is committed now so whoever picks that fix up can prove it.
//
// What it pins: indexing the same corpus twice must produce the same graph. It
// does not today. Four runs of the same fixture corpus produced pre-wipe totals
// of 2,506/3,294, 2,504/3,289, 2,505/3,288, and 2,507/3,300 nodes/relationships,
// differing in the EvidenceArtifact, Module, and Environment families.
//
// The mechanism this test isolates: aggregateCandidate keeps the first five
// evidence facts it happens to see as the candidate's evidence_preview. Nothing
// sorts the facts first, so the preview — and therefore the candidate Details
// that flow into the projected graph — depends on the order rows came back from
// Postgres rather than on their content. Feed one candidate more than five facts
// in two different orders and the two runs disagree.
//
// The located fix is to sort a candidate's facts by a content key (confidence
// descending, then evidence kind, path, matched value) before the five-item cap
// in this file, and longer term to make relationship generation identity
// content-addressed. That touches projected graph truth and the golden snapshot,
// so it is deliberately a separate change from #4594.
//
// It runs un-skipped since #6184: aggregateCandidate sorts its facts by a
// content key before accumulating, so forward and reversed inputs must agree.
func TestAggregateCandidateEvidencePreviewIsOrderIndependent(t *testing.T) {
	t.Parallel()

	key := entityTriple{
		SourceEntityID:   "repo:source",
		TargetEntityID:   "repo:target",
		RelationshipType: RelationshipType("DEPENDS_ON"),
	}

	// Seven facts for one candidate, so the five-item preview cap has to choose.
	// Confidences differ so a content-ordered cap has an unambiguous answer.
	facts := []EvidenceFact{
		newOrderProbeFact("TERRAFORM_MODULE_SOURCE", "modules.tf", "a", 0.91),
		newOrderProbeFact("TERRAFORM_GITHUB_REPOSITORY", "main.tf", "b", 0.98),
		newOrderProbeFact("HELM_CHART_DEPENDENCY", "Chart.yaml", "c", 0.72),
		newOrderProbeFact("PACKAGE_MANIFEST", "package.json", "d", 0.65),
		newOrderProbeFact("GITHUB_WORKFLOW_USES", "ci.yml", "e", 0.55),
		newOrderProbeFact("DOCKERFILE_FROM", "Dockerfile", "f", 0.44),
		newOrderProbeFact("SUBMODULE_PIN", ".gitmodules", "g", 0.33),
	}

	shuffled := make([]EvidenceFact, len(facts))
	copy(shuffled, facts)
	// A deterministic reversal, not a random shuffle: a seeded shuffle would make
	// this test's own failure depend on the seed, and reversal is enough to prove
	// the aggregation reads order rather than content.
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	forward := aggregateCandidate(key, facts)
	reversed := aggregateCandidate(key, shuffled)

	if !reflect.DeepEqual(forward.Details["evidence_preview"], reversed.Details["evidence_preview"]) {
		t.Fatalf("evidence_preview depends on input order, so two indexing runs of the same "+
			"facts produce different candidate details and a different graph.\n"+
			"forward:  %v\nreversed: %v",
			forward.Details["evidence_preview"], reversed.Details["evidence_preview"])
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("aggregateCandidate is not a pure function of its fact set:\nforward:  %+v\nreversed: %+v",
			forward, reversed)
	}
}

// TestAggregateCandidateRationaleAndRepoOrderIndependent closes the F1 gap
// from the #6184 review: rationale join order and first-non-empty repo IDs
// are order-sensitive accumulations that live outside Details, so two facts
// identical on every preview-relevant field but differing in rationale or
// repo IDs must still aggregate identically in both input orders.
func TestAggregateCandidateRationaleAndRepoOrderIndependent(t *testing.T) {
	t.Parallel()

	key := entityTriple{
		SourceEntityID:   "repo:source",
		TargetEntityID:   "repo:target",
		RelationshipType: RelationshipType("DEPENDS_ON"),
	}
	twin := func(rationale, srcRepo, tgtRepo string) EvidenceFact {
		return EvidenceFact{
			EvidenceKind:     EvidenceKind("PACKAGE_MANIFEST"),
			RelationshipType: RelationshipType("DEPENDS_ON"),
			SourceRepoID:     srcRepo,
			TargetRepoID:     tgtRepo,
			SourceEntityID:   "repo:source",
			TargetEntityID:   "repo:target",
			Confidence:       0.65,
			Rationale:        rationale,
			Details: map[string]any{
				"path":          "package.json",
				"matched_value": "d",
			},
		}
	}
	forward := []EvidenceFact{
		twin("first rationale", "repo:a", "repo:x"),
		twin("second rationale", "repo:b", "repo:y"),
	}
	reversed := []EvidenceFact{forward[1], forward[0]}

	gotForward := aggregateCandidate(key, forward)
	gotReversed := aggregateCandidate(key, reversed)
	if !reflect.DeepEqual(gotForward, gotReversed) {
		t.Fatalf("aggregateCandidate depends on input order for rationale/repo twins:\nforward:  %+v\nreversed: %+v",
			gotForward, gotReversed)
	}
}

// TestAggregateCandidateNaNConfidenceOrderIndependent closes the codex P2
// from the #6184 review: the raw-confidence tie-break compares unclamped
// values with !=, which is always true for NaN, but NaN > x is always false,
// so a NaN fact short-circuits to arrival order without consulting the
// deeper content keys (Details, rationale, repo IDs). Two facts that differ
// only past the NaN must still aggregate identically in both input orders.
func TestAggregateCandidateNaNConfidenceOrderIndependent(t *testing.T) {
	t.Parallel()

	key := entityTriple{
		SourceEntityID:   "repo:source",
		TargetEntityID:   "repo:target",
		RelationshipType: RelationshipType("DEPENDS_ON"),
	}
	twin := func(confidence float64, rationale, srcRepo string) EvidenceFact {
		return EvidenceFact{
			EvidenceKind:     EvidenceKind("PACKAGE_MANIFEST"),
			RelationshipType: RelationshipType("DEPENDS_ON"),
			SourceRepoID:     srcRepo,
			TargetRepoID:     "repo:target",
			SourceEntityID:   "repo:source",
			TargetEntityID:   "repo:target",
			Confidence:       confidence,
			Rationale:        rationale,
			Details: map[string]any{
				"path":          "package.json",
				"matched_value": "d",
			},
		}
	}
	// NaN clamps to 0, tying with the 0.0 fact on clamped confidence, kind,
	// path, and matched value; rationale and repo differ past the raw
	// tie-break. A NaN/NaN pair exercises the same short-circuit with no
	// magnitude on either side.
	forward := []EvidenceFact{
		twin(math.NaN(), "nan rationale", "repo:nan"),
		twin(0, "zero rationale", "repo:zero"),
		twin(math.NaN(), "second nan rationale", "repo:nan2"),
	}
	reversed := []EvidenceFact{forward[2], forward[1], forward[0]}

	gotForward := aggregateCandidate(key, forward)
	gotReversed := aggregateCandidate(key, reversed)
	// %#v, not reflect.DeepEqual: DeepEqual(NaN, NaN) is always false, so
	// the preview's unclamped NaN confidences would fail a DeepEqual even
	// for identical orderings. %#v prints NaN deterministically, so this
	// still proves the aggregation is a pure function of the fact set.
	if fmt.Sprintf("%#v", gotForward) != fmt.Sprintf("%#v", gotReversed) {
		t.Fatalf("aggregateCandidate depends on input order for NaN-confidence facts:\nforward:  %+v\nreversed: %+v",
			gotForward, gotReversed)
	}
}

// newOrderProbeFact builds one evidence fact for the order-independence probe.
func newOrderProbeFact(kind, path, matched string, confidence float64) EvidenceFact {
	return EvidenceFact{
		EvidenceKind:     EvidenceKind(kind),
		RelationshipType: RelationshipType("DEPENDS_ON"),
		SourceRepoID:     "repo:source",
		TargetRepoID:     "repo:target",
		SourceEntityID:   "repo:source",
		TargetEntityID:   "repo:target",
		Confidence:       confidence,
		Rationale:        kind,
		Details: map[string]any{
			"path":          path,
			"matched_value": matched,
		},
	}
}
