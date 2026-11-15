// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// severityObservation returns the observation whose severity the product path
// reports, given the representative that names the finding. The product path
// reads one row, and the representative is chosen by source rank alone; the CVE
// List ranks above NVD, so an unscored cve_list row would otherwise hide NVD's
// score (eshu-hq/eshu#10717). This mirrors the package path's severity
// fallback: a source-reported score beats a vector-derived score, which beats a
// label, and source rank orders observations within one tier. A withdrawn or
// rejected record lends no severity. Identity never reads this row: the
// finding's advisory id, dates, and key stay the representative's. With no
// better-tiered observation the representative itself is returned, so a group
// whose ranked row already carries the best severity is unchanged.
func (g supplyChainCVEGroup) severityObservation(representative supplychainmodel.ImpactCVE) supplychainmodel.ImpactCVE {
	best := representative
	bestTier := observationSeverityTier(representative)
	bestRank := observationSourceRank(representative)
	for _, observation := range g.observations {
		if advisoryRetractionCode(observation.Status, observation.WithdrawnAt) != "" {
			continue
		}
		tier := observationSeverityTier(observation)
		if tier > bestTier {
			continue
		}
		rank := observationSourceRank(observation)
		if tier < bestTier || rank < bestRank || (rank == bestRank && observation.FactID < best.FactID) {
			best, bestTier, bestRank = observation, tier, rank
		}
	}
	return best
}

// observationSeverityTier orders what an observation says about severity, the
// lowest tier winning: a reported score, a score derived from its vector, a
// recognized label, then nothing. It is the tiering of selectFallbackSeverity,
// read through the same cvss.DeriveSeverity the finding uses.
func observationSeverityTier(observation supplychainmodel.ImpactCVE) int {
	// A retracted representative still names the finding, but it earns no tier
	// of its own, so any live observation with a severity replaces it.
	if advisoryRetractionCode(observation.Status, observation.WithdrawnAt) != "" {
		return 4
	}
	severity := cvss.DeriveSeverity(observation.CVSSScore, observation.CVSSVector, observation.SeverityLabel)
	switch {
	case severity.Score > 0 && severity.ScoreSource == cvss.ScoreSourceReported:
		return 0
	case severity.Score > 0:
		return 1
	case severity.Bucket != cvss.BucketNone:
		return 2
	default:
		return 3
	}
}

func observationSourceRank(observation supplychainmodel.ImpactCVE) int {
	return advisorySourcePriority("", classifyAdvisorySource(observation.Source, observation.AdvisoryID))
}
