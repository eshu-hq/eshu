// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import "github.com/eshu-hq/eshu/go/internal/telemetry"

// SuccessorGuardInput is everything the #7926 destructive-successor backstop
// decides one commit on (#8750). Counts are like for like: PriorCollector is
// the prior's collector rows (reducer-derived rows excluded), and the delta
// fields size a delta successor by the file-level union the projector will
// build, never by the delta's own stream.
type SuccessorGuardInput struct {
	// PriorFound reports whether the scope has a prior generation.
	PriorFound bool
	// PriorCollector is the prior generation's collector row count.
	PriorCollector int
	// PriorOrphan marks a prior delta whose base is missing (#8362), sized
	// on its own rows.
	PriorOrphan bool
	// SuccessorFacts is the successor's streamed fact count.
	SuccessorFacts int
	// TrustedDeletion is the collector's attestation that a shrink is real.
	TrustedDeletion bool

	// Delta marks a delta successor that streamed its delta_generation
	// repository fact. A generation that claims IsDelta without that fact
	// is passed with Delta false and sized as a full.
	Delta bool
	// AnchorMissing reports that no projected full generation exists at the
	// delta's baseline commit.
	AnchorMissing bool
	// AnchorFiles is the base full generation's live file rows.
	AnchorFiles int
	// TouchedOrLive is |touched paths ∪ successor live file paths|.
	TouchedOrLive int
	// LiveFiles is the successor's live (non-tombstone) file paths.
	LiveFiles int
}

// SuccessorGuardDecision is the backstop's verdict for one commit. When
// NeedsProbe is set the zero-query bound failed: the caller counts the base
// file paths inside TouchedOrLive and finishes with DecideDeltaFileUnion.
type SuccessorGuardDecision struct {
	// Refuse reports that the successor must not be stored.
	Refuse bool
	// NeedsProbe asks the caller for the exact union probe.
	NeedsProbe bool
	// Rule is the deciding arm, one of the closed telemetry.SuccessorGuardRule*.
	Rule string
	// UnionLowerBound is the zero-query lower bound on the union's file
	// count, set for delta successors with an anchor.
	UnionLowerBound int
}

// DecideSuccessor applies the backstop. The arms, in order:
//
//   - trusted deletion stores;
//   - no prior, or a prior with no collector rows, stores (nothing to
//     retract) — except an orphan prior under an empty successor, which is
//     refused (#8362);
//   - an empty successor over a content-bearing prior is refused;
//   - a full successor is refused when it holds under half the prior's
//     collector rows;
//   - a delta with no anchor stores: the projector refuses it as
//     base-missing and arms the reindex watermark, which a store-side
//     refusal would not;
//   - a delta whose union keeps at least half the anchor's files by the
//     zero-query lower bound stores; otherwise the caller probes.
//
// The lower bound subtracts every touched or live path as if each were an
// anchor file, so it can only under-estimate the union: a failed bound means
// "probe", never "refuse".
func DecideSuccessor(in SuccessorGuardInput) SuccessorGuardDecision {
	if in.TrustedDeletion {
		return SuccessorGuardDecision{Rule: telemetry.SuccessorGuardRuleTrustedDeletion}
	}
	if in.PriorOrphan && in.SuccessorFacts <= 0 {
		return SuccessorGuardDecision{Refuse: true, Rule: telemetry.SuccessorGuardRuleOrphanFallback}
	}
	if !in.PriorFound || in.PriorCollector <= 0 {
		return SuccessorGuardDecision{Rule: telemetry.SuccessorGuardRuleNoPrior}
	}
	if in.SuccessorFacts <= 0 {
		return SuccessorGuardDecision{Refuse: true, Rule: telemetry.SuccessorGuardRuleEmpty}
	}
	if !in.Delta {
		rule := telemetry.SuccessorGuardRuleCollectorRatio
		if in.PriorOrphan {
			rule = telemetry.SuccessorGuardRuleOrphanFallback
		}
		refuse, _ := RefuseDestructiveSuccessor(true, in.PriorCollector, in.SuccessorFacts, false)
		return SuccessorGuardDecision{Refuse: refuse, Rule: rule}
	}
	if in.AnchorMissing {
		return SuccessorGuardDecision{Rule: telemetry.SuccessorGuardRuleAnchorMissing}
	}
	lower := in.AnchorFiles - in.TouchedOrLive + in.LiveFiles
	decision := SuccessorGuardDecision{Rule: telemetry.SuccessorGuardRuleFileUnionBound, UnionLowerBound: lower}
	if in.AnchorFiles <= 0 || lower*2 >= in.AnchorFiles {
		return decision
	}
	decision.NeedsProbe = true
	decision.Rule = telemetry.SuccessorGuardRuleFileUnionExact
	return decision
}

// DecideDeltaFileUnion finishes a probed delta: anchorFilesTouched is how
// many of the anchor's files lie in the touched-or-live set. The union keeps
// every other anchor file and adds the delta's live files; a union under half
// the anchor's files is a sharp shrink and is refused. It returns the union
// file count for the operator log.
func DecideDeltaFileUnion(anchorFiles, anchorFilesTouched, liveFiles int) (refuse bool, unionFiles int) {
	unionFiles = anchorFiles - anchorFilesTouched + liveFiles
	return unionFiles*2 < anchorFiles, unionFiles
}
