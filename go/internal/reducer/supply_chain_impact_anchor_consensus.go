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

// buildSupplyChainImageIdentityConsensus counts, for every
// reducer_container_image_identity envelope in the batch, how many rows at
// each digest/tier corroborate each candidate repository
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
func buildSupplyChainImageIdentityConsensus(envelopes []facts.Envelope) map[supplyChainImageIdentityConsensusKey]int {
	counts := make(map[supplyChainImageIdentityConsensusKey]int)
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		row := supplyChainImageIdentityFromEnvelope(envelope)
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
// observed_at (go/internal/collector/git_source_processing.go:
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
// makes the anchor stable too. When counts also tie, or when either row's
// repository does not resolve, or the two rows resolve to the SAME
// repository, this defers to the bare preferSupplyChainImageIdentity
// (unchanged behavior for every case the existing tier tests already pin --
// none of the pre-#5887 tests exercise two same-tier rows naming DIFFERENT
// repositories, only same-tier-same-repository or cross-tier disagreement).
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
