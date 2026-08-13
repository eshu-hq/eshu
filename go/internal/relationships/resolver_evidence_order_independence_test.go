// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
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
// It is skipped rather than left red. A permanently failing test turns every
// future run of this package red, which trains people to ignore it and blocks
// unrelated work — the opposite of a useful pin. Delete the t.Skip line to run
// it: it fails today with a diff of the two previews, and it passes when the
// ordering fix lands. That one-line edit is the proof step.
func TestAggregateCandidateEvidencePreviewIsOrderIndependent(t *testing.T) {
	t.Parallel()
	t.Skip("known-failing pin for cross-run resolver nondeterminism; delete this line to verify the fix")

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
