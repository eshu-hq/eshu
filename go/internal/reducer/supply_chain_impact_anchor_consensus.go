// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainImageIdentityConsensusKey identifies one (digest, tier,
// repository) corroboration bucket for supplyChainImageIdentityConsensus.
type supplyChainImageIdentityConsensusKey struct {
	digest       string
	tier         int
	repositoryID string
}

// buildSupplyChainImageIdentityConsensus counts, for every DISTINCT LOGICAL
// reducer_container_image_identity row in the batch (see
// dedupeContainerImageIdentityByLogicalRow), how many rows at each
// digest/tier corroborate each candidate repository
// (singleSupplyChainImageSourceRepositoryID). #5887's
// preferSupplyChainImageIdentityConsensus uses this count instead of factID
// to break a same-tier disagreement, so the winner is decided by how many
// writers agree rather than by a hash that embeds a per-run generation_id.
//
// This is a full pre-pass over the batch (both real call sites --
// bestSupplyChainImageIdentitiesByDigest and
// buildSupplyChainImpactIndexWithQuarantine -- already receive the complete
// envelope slice up front, so this adds one more O(N) pass, not a new
// streaming/ordering dependency).
//
// The map is pre-sized to len(envelopes): the true entry count is bounded by
// the number of distinct (digest, tier, repositoryID) keys, which is at most
// one per logical row and usually far fewer once rows collapse onto shared
// keys, so this only avoids rehashing on the way up -- it never
// over-allocates relative to the batch this function already iterates.
func buildSupplyChainImageIdentityConsensus(envelopes []facts.Envelope) map[supplyChainImageIdentityConsensusKey]int {
	logicalRows := dedupeContainerImageIdentityByLogicalRow(envelopes)
	counts := make(map[supplyChainImageIdentityConsensusKey]int, len(logicalRows))
	for _, row := range logicalRows {
		if row.digest == "" {
			continue
		}
		repositoryID := singleSupplyChainImageSourceRepositoryID(row)
		if repositoryID == "" {
			continue
		}
		key := supplyChainImageIdentityConsensusKey{
			digest:       row.digest,
			tier:         supplyChainImageIdentityAnchorTier(row),
			repositoryID: repositoryID,
		}
		counts[key]++
	}
	return counts
}

// containerImageIdentityLogicalKey identifies one WRITER's decision for one
// image reference: (scope_id, generation_id, image_ref) -- the outcome-
// independent v2 identity containerImageIdentityIdentity computes
// (container_image_identity_writer.go) -- plus digest. A legacy and a v2 row
// for the SAME decision always agree on digest (both describe one decision's
// resolution of one image_ref), so including it does not stop the legacy/v2
// pair this key exists to collapse from colliding; it only additionally
// disambiguates genuinely distinct decisions that happen to share a blank or
// otherwise-colliding scope/generation/image_ref, which every non-anchor
// test fixture in this package does (they never set ScopeID/GenerationID or
// an image_ref payload field). Without digest in the key, this dedup would
// collapse those unrelated rows onto each other instead of only collapsing
// genuine legacy/v2 duplicates.
type containerImageIdentityLogicalKey struct {
	scopeID      string
	generationID string
	imageRef     string
	digest       string
}

// containerImageIdentityLogicalCandidate pairs a decoded row with whether it
// was read from a v2-format fact (identity_format ==
// containerImageIdentityFormatImageRef), so
// dedupeContainerImageIdentityByLogicalRow can prefer the canonical
// representation when both a legacy and a v2 row exist for the same logical
// key.
type containerImageIdentityLogicalCandidate struct {
	row  supplyChainImageIdentity
	isV2 bool
}

// dedupeContainerImageIdentityByLogicalRow collapses every
// reducer_container_image_identity envelope down to at most one row per
// logical identity, so buildSupplyChainImageIdentityConsensus counts one
// vote per WRITER decision rather than one vote per stored fact ID (#5887
// follow-up).
//
// WriteContainerImageIdentityDecisions' own doc comment
// (container_image_identity_writer.go) explains why a single logical
// decision can be stored under two DIFFERENT fact IDs at once: during the
// #5854 v2 cutover, a completeness warning can deliberately hold a legacy
// outcome-keyed row live while the same completed pass publishes the
// stronger v2 row for the identical (scope_id, generation_id, image_ref).
// Both rows land in the same envelope batch this function reads. Counting
// per envelope let that ONE decision cast TWO votes for its repository while
// the legacy row was held, and the count silently dropped back to one --
// potentially flipping which repository had the most votes, and therefore
// the anchor and the finding's own identity (supplyChainImpactLogicalIdentity
// includes RepositoryID) -- the instant a later pass retired the legacy row.
// That is the same class of bug #5887 fixed (the anchor moving with no
// change in underlying evidence), reached through the cutover boundary
// instead of a per-run factID draw. Deduplicating on the logical key here
// closes it the same way: the vote no longer depends on how many physical
// fact IDs happen to represent one decision at read time.
//
// When both a legacy and a v2 row exist for the same logical key, the v2 row
// is kept as the representative -- it is the fresher, canonical write, and
// cleanup will eventually retire the legacy row anyway. When only a legacy
// row exists (no v2 publication yet, or cutover not reached), it is kept: it
// is the only evidence available. A tie between two rows of the same format
// (defensive; the writer's upsert on the v2 key should never produce two
// live v2 rows for one logical key) resolves by the smaller factID -- an
// arbitrary, extremely unlikely-to-be-reached case, not the primary rule.
func dedupeContainerImageIdentityByLogicalRow(envelopes []facts.Envelope) []supplyChainImageIdentity {
	byLogicalKey := make(map[containerImageIdentityLogicalKey]containerImageIdentityLogicalCandidate, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		row := supplyChainImageIdentityFromEnvelope(envelope)
		candidate := containerImageIdentityLogicalCandidate{
			row:  row,
			isV2: payloadStr(envelope.Payload, "identity_format") == containerImageIdentityFormatImageRef,
		}
		key := containerImageIdentityLogicalKey{
			scopeID:      envelope.ScopeID,
			generationID: envelope.GenerationID,
			imageRef:     row.imageRef,
			digest:       row.digest,
		}
		current, ok := byLogicalKey[key]
		if !ok || preferContainerImageIdentityLogicalCandidate(candidate, current) {
			byLogicalKey[key] = candidate
		}
	}
	rows := make([]supplyChainImageIdentity, 0, len(byLogicalKey))
	for _, candidate := range byLogicalKey {
		rows = append(rows, candidate.row)
	}
	return rows
}

// preferContainerImageIdentityLogicalCandidate reports whether candidate
// should replace current as the representative row for one logical key: a
// v2 row always beats a legacy row, and within the same format the smaller
// factID wins (a defensive, deterministic last resort -- see
// dedupeContainerImageIdentityByLogicalRow's doc for why this branch should
// not be reachable in normal operation).
func preferContainerImageIdentityLogicalCandidate(
	candidate, current containerImageIdentityLogicalCandidate,
) bool {
	if candidate.isV2 != current.isV2 {
		return candidate.isV2
	}
	return candidate.row.factID < current.row.factID
}

// preferSupplyChainImageIdentityConsensus is the #5887 consensus-aware
// replacement bestSupplyChainImageIdentitiesByDigest and
// addSupplyChainImpactIndexEntry use in place of a bare
// preferSupplyChainImageIdentity fold.
//
// #5854 made reducer_container_image_identity's fact ID
// outcome-independent, and a scope's canonical decision can collapse to a
// single source_repository_ids entry -- flipping a row from tier B/C into
// tier A. Once two tier A rows for the same digest disagree on repository,
// bare preferSupplyChainImageIdentity's tie-break compares factID, a
// SHA-256 whose input embeds generation_id
// (containerImageIdentityIdentity). For rows derived from a live git
// snapshot, generation_id is stableID'd from the collector run's wall-clock
// observed_at (go/internal/collector/gitrepo/git_source_processing.go:
// GitCollectorSnapshotRun -> sourceRunID -> buildGeneration), so that
// factID is a fresh, unpredictable draw every run: the SAME two
// disagreeing rows can pick a different winner run to run even though
// nothing about the underlying evidence changed. See issue #5887.
//
// This function keeps the tier check exactly as
// supplyChainImageIdentityAnchorTier/preferSupplyChainImageIdentity already
// define it (tier A > tier B > tier C, per #5813) -- that precedence is
// unchanged. It only replaces the SAME-TIER, DIFFERENT-REPOSITORY
// tie-break: instead of the smaller factID, the repository with more
// corroborating rows in that tier wins (consensus, computed once per batch
// by buildSupplyChainImageIdentityConsensus). A run's SET of writers and
// what they assert does not depend on any per-run nonce, so this count is
// stable across runs and envelope order for a fixed corpus, which is what
// makes the anchor stable too.
//
// When either row's repository does not resolve, or the two rows resolve to
// the SAME repository, this defers to the bare preferSupplyChainImageIdentity
// (unchanged behavior for every case the existing tier tests already pin --
// none of the pre-#5887 tests exercise two same-tier rows naming DIFFERENT
// repositories, only same-tier-same-repository or cross-tier disagreement).
// That deferral is safe specifically because the repository is already
// settled (or unsettled on both sides) before the bare function's factID
// comparison ever runs, so its per-run instability cannot move the resolved
// repository there -- only which specific row's factID gets cited as
// evidence.
//
// When corroboration counts ALSO tie between two DISTINCT repositories, this
// function does NOT fall through to the bare factID comparison: it breaks
// the tie by comparing the repository ID strings themselves (below). That is
// still not a per-run value, but it does decide the winning repository, so
// do not describe this path as deferring to preferSupplyChainImageIdentity.
func preferSupplyChainImageIdentityConsensus(
	existing, candidate supplyChainImageIdentity,
	consensus map[supplyChainImageIdentityConsensusKey]int,
) supplyChainImageIdentity {
	existingTier := supplyChainImageIdentityAnchorTier(existing)
	candidateTier := supplyChainImageIdentityAnchorTier(candidate)
	if existingTier != candidateTier {
		return preferSupplyChainImageIdentity(existing, candidate)
	}

	existingRepositoryID := singleSupplyChainImageSourceRepositoryID(existing)
	candidateRepositoryID := singleSupplyChainImageSourceRepositoryID(candidate)
	if existingRepositoryID == "" || candidateRepositoryID == "" || existingRepositoryID == candidateRepositoryID {
		return preferSupplyChainImageIdentity(existing, candidate)
	}

	existingCount := consensus[supplyChainImageIdentityConsensusKey{
		digest: existing.digest, tier: existingTier, repositoryID: existingRepositoryID,
	}]
	candidateCount := consensus[supplyChainImageIdentityConsensusKey{
		digest: candidate.digest, tier: candidateTier, repositoryID: candidateRepositoryID,
	}]
	if existingCount != candidateCount {
		if candidateCount > existingCount {
			return candidate
		}
		return existing
	}

	// Exact corroboration tie between two DISTINCT repositories: fall back
	// to comparing the repository id strings themselves. Unlike factID,
	// this is a stable, content-derived git-repository identifier that
	// never embeds a per-run generation_id, so -- unlike the bare factID
	// fallback -- the winner stays fixed for a given corpus no matter which
	// run computed it. This path is a last resort for a genuine evidence
	// deadlock (equal corroboration on both sides), not the primary
	// decision rule.
	if candidateRepositoryID < existingRepositoryID {
		return candidate
	}
	return existing
}
