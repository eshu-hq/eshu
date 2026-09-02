// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// The cross-repo resolution family moved to [crossrepo] under issue #6061.
// These aliases keep the reducer root's own callers -- and the packages that
// name them through this package (cmd/reducer, internal/ifa/materializededges,
// internal/storage/cypher) -- compiling against the same spellings. The
// dependency runs root -> family only; the family never imports this package.

// CrossRepoEvidenceSource is the evidence_source the cross-repo resolver stamps
// on every edge it writes. Alias for [crossrepo.CrossRepoEvidenceSource].
const CrossRepoEvidenceSource = crossrepo.CrossRepoEvidenceSource

// EvidenceFactLoader loads persisted evidence facts for a generation.
// Alias for [crossrepo.EvidenceFactLoader].
type EvidenceFactLoader = crossrepo.EvidenceFactLoader

// AssertionLoader loads relationship assertions.
// Alias for [crossrepo.AssertionLoader].
type AssertionLoader = crossrepo.AssertionLoader

// ResolutionPersister persists resolution outputs and activates the generation.
// Alias for [crossrepo.ResolutionPersister].
type ResolutionPersister = crossrepo.ResolutionPersister

// RepoDependencyIntentWriter persists durable repo-dependency projection
// intents. Alias for [crossrepo.RepoDependencyIntentWriter].
type RepoDependencyIntentWriter = crossrepo.RepoDependencyIntentWriter

// CrossRepoRelationshipHandler resolves cross-repository relationships from
// persisted evidence facts. Alias for [crossrepo.CrossRepoRelationshipHandler].
type CrossRepoRelationshipHandler = crossrepo.CrossRepoRelationshipHandler

// ExtractRepoDependencyIntentRows exposes the resolved-relationship to
// intent-row conversion for Ifá's materialized-edge vacuity guards. Forwards to
// [crossrepo.ExtractRepoDependencyIntentRows].
func ExtractRepoDependencyIntentRows(
	resolved []relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) ([]sharedintent.Row, map[string]int) {
	return crossrepo.ExtractRepoDependencyIntentRows(resolved, scopeID, sourceRunID, generationID, createdAt)
}
