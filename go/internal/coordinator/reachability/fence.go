// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
)

// FenceReasonStaleEvidence labels a generation retracted because the repository
// moved off the commit it measured and no successor landed in time. It is the
// only value of the fence metric's reason label today.
const FenceReasonStaleEvidence = "stale_evidence"

// staleEvidenceRotations is the commit-skew bound in rotation periods. It is fixed
// by the eshu-hq/eshu#10055 ruling and deliberately not configurable: skew this old
// was visible at two earlier visits, each of which planned a successor that did not
// land. The first period is the visit; the second covers the claim, its retries, and
// the slot jitter when the fleet size crosses a page boundary.
const staleEvidenceRotations = 2

// FenceDecision is what one page's commit-skew check found.
type FenceDecision struct {
	// Fences names the generations to retract.
	Fences []scannercomponent.StaleEvidenceFence
	// SkippedNoCommit counts generations holding Go evidence with no stamped
	// commit. Skew cannot be shown for them, so they are never retracted; each is
	// re-scanned on its next successful claim and then carries the commit.
	SkippedNoCommit int
	// HistoryPruned counts the Fences whose evidence commit has no surviving
	// superseded generation. Retention prunes only rows older than its cutoff, so
	// the skew is at least that old.
	HistoryPruned int
}

// StaleEvidence applies the commit-skew rule to one page, using the slot count read
// with that page. A derived generation is fenced when it holds Go evidence, its
// commit differs from the repository's active commit, and the repository left that
// commit more than two rotation periods ago. One repository is visited once per
// slotCount x interval, so the bound is in rotations, not intervals.
//
// The age is measured from the repository's own history, never from the evidence's
// activation: a quiet repository's evidence is old by design, because an Unchanged
// claim commits nothing, and it stays accurate for its commit whatever the remote
// does. Evidence with no surviving history fences at once, because retention proves
// the skew is old.
//
// The rule is blind to why no successor landed. A dead remote, a revoked credential,
// a dead-lettered claim, and a stopped scanner all reach the same bound. It runs on
// the page the planner already read, reads nothing, and writes nothing.
func StaleEvidence(
	repositories []scannercomponent.GoModuleRepository,
	observedAt time.Time,
	interval time.Duration,
	slotCount int,
) FenceDecision {
	bound := staleEvidenceRotations * time.Duration(max(slotCount, 1)) * interval
	var decision FenceDecision
	for _, repository := range repositories {
		if !repository.HasGoEvidence || repository.EvidenceGenerationID == "" {
			continue
		}
		if repository.EvidenceCommitSHA == "" {
			decision.SkippedNoCommit++
			continue
		}
		if repository.SourceCommitSHA == "" || repository.SourceCommitSHA == repository.EvidenceCommitSHA {
			continue
		}
		pruned := repository.EvidenceLeftAt.IsZero()
		if !pruned && observedAt.Sub(repository.EvidenceLeftAt) <= bound {
			continue
		}
		scopeID, err := scannercomponent.GoReachabilityScopeID(repository.RepositoryID)
		if err != nil {
			continue
		}
		decision.Fences = append(decision.Fences, scannercomponent.StaleEvidenceFence{
			ScopeID:      scopeID,
			GenerationID: repository.EvidenceGenerationID,
		})
		if pruned {
			decision.HistoryPruned++
		}
	}
	return decision
}
