// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
)

func containerImageIdentityWriteResult(
	canonicalWrites int,
	retirementAttempts int,
	legacyRowsDeleted int,
) ContainerImageIdentityWriteResult {
	return ContainerImageIdentityWriteResult{
		CanonicalWrites:    canonicalWrites,
		RetirementAttempts: retirementAttempts,
		LegacyRowsDeleted:  legacyRowsDeleted,
		EvidenceSummary: fmt.Sprintf(
			"wrote container image identity decisions %d attempted retirements %d legacy rows deleted %d",
			canonicalWrites,
			retirementAttempts,
			legacyRowsDeleted,
		),
	}
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

func containerImageIdentityPublicationRank(outcome ContainerImageIdentityOutcome) int {
	switch outcome {
	case ContainerImageIdentityExactDigest:
		return 2
	case ContainerImageIdentityTagResolved:
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
