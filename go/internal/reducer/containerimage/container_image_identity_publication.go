// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"fmt"
	"sort"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func containerImageIdentityWriteResult(
	canonicalWrites int,
	retirementAttempts int,
	legacyRowsDeleted int,
	supportChanged bool,
) ContainerImageIdentityWriteResult {
	return ContainerImageIdentityWriteResult{
		CanonicalWrites:    canonicalWrites,
		RetirementAttempts: retirementAttempts,
		LegacyRowsDeleted:  legacyRowsDeleted,
		SupportChanged:     supportChanged,
		EvidenceSummary: fmt.Sprintf(
			"wrote container image identity decisions %d attempted retirements %d legacy rows deleted %d",
			canonicalWrites,
			retirementAttempts,
			legacyRowsDeleted,
		),
	}
}

// SupportChangedSignal is the Result.SubSignals key carrying whether this
// run's support-set publish changed the scope's durable support truth for
// the identity completion ACK (#8284): the ACK emits a completion event
// only when this signal is absent (unwired producers fail open, so a
// missing gate never starves downstream consumers) or positive. Producers
// report an explicit zero when the republished set is byte-identical to the
// previous last_set_id so the ACK can tell "unchanged, suppress" from
// "gate not wired".
const SupportChangedSignal = "support_changed"

// WithSupportChangedSignal sets SupportChangedSignal on an existing
// SubSignals map, allocating one when the caller built none. The signal is
// always explicit (even zero) for the reason above.
func WithSupportChangedSignal(signals map[string]float64, changed bool) map[string]float64 {
	if signals == nil {
		signals = make(map[string]float64, 1)
	}
	if changed {
		signals[SupportChangedSignal] = 1
	} else {
		signals[SupportChangedSignal] = 0
	}
	return signals
}

// ShouldEmitIdentityCompletion decides whether an identity ACK emits a
// cross-scope completion event. Unlike the value-flow refresh gate
// (affected.ShouldEmitRefresh), CanonicalWrites is deliberately not
// consulted: the identity handler republishes the scope's complete support
// set on every run, so a positive write count cannot tell a changed set
// from a byte-identical replay — only the previous-last_set_id comparison
// the writer reports through SupportChangedSignal can.
func ShouldEmitIdentityCompletion(result reducercontract.Result) bool {
	changed, ok := result.SubSignals[SupportChangedSignal]
	if !ok {
		return true
	}
	return changed > 0
}

type containerImageIdentityPublication struct {
	decision  ContainerImageIdentityDecision
	tombstone bool
}

func planContainerImageIdentityPublications(
	write ContainerImageIdentityWrite,
) []containerImageIdentityPublication {
	byFactID := make(map[string]containerImageIdentityPublication)
	for _, decision := range containerImageIdentityCanonicalDecisions(write.Decisions) {
		publication := containerImageIdentityPublication{decision: decision}
		factID := containerImageIdentityFactID(write, decision)
		if current, ok := byFactID[factID]; !ok ||
			preferContainerImageIdentityPublication(publication, current) {
			byFactID[factID] = publication
		}
	}
	for _, decision := range write.TombstoneDecisions {
		publication := containerImageIdentityPublication{
			decision:  decision,
			tombstone: true,
		}
		factID := containerImageIdentityFactID(write, decision)
		if current, ok := byFactID[factID]; !ok ||
			preferContainerImageIdentityPublication(publication, current) {
			byFactID[factID] = publication
		}
	}

	factIDs := make([]string, 0, len(byFactID))
	for factID := range byFactID {
		factIDs = append(factIDs, factID)
	}
	sort.Strings(factIDs)
	publications := make([]containerImageIdentityPublication, 0, len(factIDs))
	for _, factID := range factIDs {
		publications = append(publications, byFactID[factID])
	}
	return publications
}

func preferContainerImageIdentityPublication(
	candidate containerImageIdentityPublication,
	current containerImageIdentityPublication,
) bool {
	if candidate.tombstone != current.tombstone {
		return !candidate.tombstone
	}
	candidateRank := containerImageIdentityPublicationRank(candidate.decision.Outcome)
	currentRank := containerImageIdentityPublicationRank(current.decision.Outcome)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	return containerImageIdentityDecisionSortKey(candidate.decision) <
		containerImageIdentityDecisionSortKey(current.decision)
}

func containerImageIdentityPublicationRank(outcome reducercontract.ContainerImageIdentityOutcome) int {
	switch outcome {
	case reducercontract.ContainerImageIdentityExactDigest:
		return 2
	case reducercontract.ContainerImageIdentityTagResolved:
		return 1
	default:
		return 0
	}
}

func containerImageIdentityDecisionSortKey(decision ContainerImageIdentityDecision) string {
	return strings.Join([]string{
		strings.TrimSpace(decision.ImageRef),
		string(decision.Outcome),
		strings.TrimSpace(decision.Digest),
		strings.TrimSpace(decision.RepositoryID),
		strings.TrimSpace(decision.SourceRevision),
		strings.TrimSpace(decision.Reason),
	}, "\x00")
}
