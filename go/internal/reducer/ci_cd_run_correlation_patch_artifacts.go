// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func cicdArtifactPatchDirectivesFromCurrent(
	envelopes []facts.Envelope,
) (cicdArtifactPatchDirectives, error) {
	liveKeys := make(map[cicdRunCorrelationPatchKey]struct{})
	liveStableKeys := make(map[string]struct{})
	tombstoneStableKeys := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDArtifactFactKind {
			continue
		}
		key, decoded := cicdArtifactPatchKeyFromEnvelope(envelope)
		if !envelope.IsTombstone {
			if stableKey := strings.TrimSpace(envelope.StableFactKey); stableKey != "" {
				liveStableKeys[stableKey] = struct{}{}
			}
			if decoded {
				liveKeys[key] = struct{}{}
			}
			continue
		}
		stableKey := strings.TrimSpace(envelope.StableFactKey)
		if stableKey == "" {
			return cicdArtifactPatchDirectives{}, fmt.Errorf(
				"load ci/cd run correlation patch: artifact tombstone %q has no stable fact key",
				envelope.FactID,
			)
		}
		tombstoneStableKeys[stableKey] = struct{}{}
	}
	return cicdArtifactPatchDirectives{
		liveRunKeys:         sortedCICDArtifactPatchRunKeys(liveKeys),
		liveStableKeys:      sortedCICDArtifactStableKeys(liveStableKeys),
		tombstoneStableKeys: sortedCICDArtifactStableKeys(tombstoneStableKeys),
	}, nil
}

func sortedCICDArtifactPatchRunKeys(
	unique map[cicdRunCorrelationPatchKey]struct{},
) []cicdRunCorrelationPatchKey {
	keys := make([]cicdRunCorrelationPatchKey, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return cicdRunCorrelationPatchKeyString(keys[i]) < cicdRunCorrelationPatchKeyString(keys[j])
	})
	return keys
}

func splitCICDArtifactPatchRunKeys(
	keys []cicdRunCorrelationPatchKey,
) ([]string, []string, []string) {
	providers := make([]string, 0, len(keys))
	runIDs := make([]string, 0, len(keys))
	runAttempts := make([]string, 0, len(keys))
	for _, key := range keys {
		providers = append(providers, key.provider)
		runIDs = append(runIDs, key.runID)
		runAttempts = append(runAttempts, key.runAttempt)
	}
	return providers, runIDs, runAttempts
}

func sortedCICDArtifactStableKeys(unique map[string]struct{}) []string {
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mergeCICDArtifactStableKeys(groups ...[]string) []string {
	unique := make(map[string]struct{})
	for _, group := range groups {
		for _, stableKey := range group {
			unique[stableKey] = struct{}{}
		}
	}
	return sortedCICDArtifactStableKeys(unique)
}

type cicdStableFactIdentity struct {
	factKind  string
	stableKey string
}

// excludeSupersededCICDFacts keeps current non-artifact facts authoritative by
// exact raw typed stable identity. Artifacts retain their normalized stable-key
// semantics, and current live artifacts additionally replace retained
// artifacts for the same run even when their stable keys differ.
func excludeSupersededCICDFacts(
	historical []facts.Envelope,
	current []facts.Envelope,
	currentArtifactKeys []cicdRunCorrelationPatchKey,
	currentArtifactStableKeys []string,
) ([]facts.Envelope, error) {
	currentArtifactKeySet := make(map[cicdRunCorrelationPatchKey]struct{}, len(currentArtifactKeys))
	for _, key := range currentArtifactKeys {
		currentArtifactKeySet[key] = struct{}{}
	}
	currentArtifactStableKeySet := make(map[string]struct{}, len(currentArtifactStableKeys))
	for _, stableKey := range currentArtifactStableKeys {
		currentArtifactStableKeySet[stableKey] = struct{}{}
	}
	currentStableIdentities := make(map[cicdStableFactIdentity]struct{}, len(current))
	for _, envelope := range current {
		if envelope.FactKind == facts.CICDArtifactFactKind {
			continue
		}
		stableKey := envelope.StableFactKey
		if envelope.IsTombstone && strings.TrimSpace(stableKey) == "" {
			return nil, fmt.Errorf(
				"load ci/cd run correlation patch: %s tombstone %q has no stable fact key",
				envelope.FactKind,
				envelope.FactID,
			)
		}
		if strings.TrimSpace(stableKey) == "" {
			continue
		}
		currentStableIdentities[cicdStableFactIdentity{
			factKind:  envelope.FactKind,
			stableKey: stableKey,
		}] = struct{}{}
	}

	filtered := make([]facts.Envelope, 0, len(historical))
	for _, envelope := range historical {
		if envelope.FactKind == facts.CICDArtifactFactKind {
			stableKey := strings.TrimSpace(envelope.StableFactKey)
			if _, superseded := currentArtifactStableKeySet[stableKey]; stableKey != "" && superseded {
				continue
			}
		} else if stableKey := envelope.StableFactKey; strings.TrimSpace(stableKey) != "" {
			identity := cicdStableFactIdentity{factKind: envelope.FactKind, stableKey: stableKey}
			if _, superseded := currentStableIdentities[identity]; superseded {
				continue
			}
		}
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if ok {
			if _, superseded := currentArtifactKeySet[key]; superseded {
				continue
			}
		}
		filtered = append(filtered, envelope)
	}
	return filtered, nil
}

func excludeCICDTombstones(envelopes []facts.Envelope) []facts.Envelope {
	filtered := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		filtered = append(filtered, envelope)
	}
	return filtered
}
