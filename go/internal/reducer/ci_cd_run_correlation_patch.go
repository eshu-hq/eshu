// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const maxCICDRunCorrelationPatchDecisions = 1_000

type cicdRunCorrelationPatchFactLoader interface {
	ListCICDRunFactsForScopePatch(
		ctx context.Context,
		scopeID string,
		targetGenerationID string,
		providers []string,
		runIDs []string,
		runAttempts []string,
	) ([]facts.Envelope, error)
}

type cicdRunCorrelationPatchKey struct {
	provider   string
	runID      string
	runAttempt string
}

type cicdArtifactPatchDirectives struct {
	liveRunKeys         []cicdRunCorrelationPatchKey
	liveStableKeys      []string
	tombstoneStableKeys []string
}

// loadCICDRunCorrelationPatchFacts expands an artifact-only generation into a
// bounded source-derived snapshot rebuild. Normal generations containing
// ci.run remain exact generation snapshots and do not make a historical query.
func (h CICDRunCorrelationHandler) loadCICDRunCorrelationPatchFacts(
	ctx context.Context,
	intent Intent,
	current []facts.Envelope,
) ([]facts.Envelope, bool, error) {
	if hasCICDFactKind(current, facts.CICDRunFactKind) || !hasCICDFactKind(current, facts.CICDArtifactFactKind) {
		return current, false, nil
	}
	loader, ok := h.FactLoader.(cicdRunCorrelationPatchFactLoader)
	if !ok {
		return nil, true, fmt.Errorf("load ci/cd run correlation patch: fact loader does not support cross-generation evidence")
	}

	directives, err := cicdArtifactPatchDirectivesFromCurrent(current)
	if err != nil {
		return nil, true, err
	}
	providers, runIDs, runAttempts := splitCICDArtifactPatchRunKeys(directives.liveRunKeys)
	historical, err := loader.ListCICDRunFactsForScopePatch(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
		providers,
		runIDs,
		runAttempts,
	)
	if err != nil {
		return nil, true, fmt.Errorf("load historical ci/cd run facts: %w", classifyFactLoadError(err))
	}
	historical, err = excludeSupersededCICDFacts(
		historical,
		current,
		directives.liveRunKeys,
		mergeCICDArtifactStableKeys(directives.liveStableKeys, directives.tombstoneStableKeys),
	)
	if err != nil {
		return nil, true, err
	}
	current = excludeCICDTombstones(current)
	combined := make([]facts.Envelope, 0, len(historical)+len(current))
	combined = append(combined, historical...)
	combined = append(combined, current...)
	return combined, true, nil
}

func hasCICDFactKind(envelopes []facts.Envelope, factKind string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			return true
		}
	}
	return false
}

func cicdArtifactPatchKeyFromEnvelope(envelope facts.Envelope) (cicdRunCorrelationPatchKey, bool) {
	if envelope.FactKind != facts.CICDArtifactFactKind {
		return cicdRunCorrelationPatchKey{}, false
	}
	artifact, err := decodeCICDArtifact(envelope)
	if err != nil {
		return cicdRunCorrelationPatchKey{}, false
	}
	key := cicdRunCorrelationPatchKey{
		provider:   trimmedCICDField(artifact.Provider),
		runID:      trimmedCICDField(artifact.RunID),
		runAttempt: defaultCICDRunAttempt(trimmedCICDPtr(artifact.RunAttempt)),
	}
	return key, key.provider != "" && key.runID != ""
}

func cicdRunCorrelationPatchKeyString(key cicdRunCorrelationPatchKey) string {
	return key.provider + "\x00" + key.runID + "\x00" + key.runAttempt
}
