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
	payloadKeys := make(map[cicdRunCorrelationPatchKey]struct{})
	tombstoneRunKeys := make(map[cicdRunCorrelationPatchKey]struct{})
	tombstoneStableKeys := make(map[string]struct{})
	unresolvedTombstoneKeys := make(map[string]struct{})
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
				payloadKeys[key] = struct{}{}
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
		if decoded {
			payloadKeys[key] = struct{}{}
			tombstoneRunKeys[key] = struct{}{}
			continue
		}
		unresolvedTombstoneKeys[stableKey] = struct{}{}
	}
	return cicdArtifactPatchDirectives{
		liveRunKeys:             sortedCICDArtifactPatchRunKeys(liveKeys),
		liveStableKeys:          sortedCICDArtifactStableKeys(liveStableKeys),
		payloadRunKeys:          sortedCICDArtifactPatchRunKeys(payloadKeys),
		tombstoneRunKeys:        sortedCICDArtifactPatchRunKeys(tombstoneRunKeys),
		tombstoneStableKeys:     sortedCICDArtifactStableKeys(tombstoneStableKeys),
		unresolvedTombstoneKeys: sortedCICDArtifactStableKeys(unresolvedTombstoneKeys),
	}, nil
}

func mergeCICDArtifactPatchRunKeys(
	groups ...[]cicdRunCorrelationPatchKey,
) []cicdRunCorrelationPatchKey {
	unique := make(map[cicdRunCorrelationPatchKey]struct{})
	for _, group := range groups {
		for _, key := range group {
			unique[key] = struct{}{}
		}
	}
	return sortedCICDArtifactPatchRunKeys(unique)
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

func resolveCICDArtifactTombstoneRunKeys(
	historical []facts.Envelope,
	requested []string,
) ([]cicdRunCorrelationPatchKey, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, stableKey := range requested {
		requestedSet[stableKey] = struct{}{}
	}
	resolved := make(map[string]cicdRunCorrelationPatchKey, len(requested))
	for _, envelope := range historical {
		stableKey := strings.TrimSpace(envelope.StableFactKey)
		if _, wanted := requestedSet[stableKey]; !wanted {
			continue
		}
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if !ok {
			continue
		}
		if prior, exists := resolved[stableKey]; exists && prior != key {
			return nil, fmt.Errorf(
				"load ci/cd run correlation patch: artifact tombstone key %q resolved to conflicting run identities",
				stableKey,
			)
		}
		resolved[stableKey] = key
	}
	keys := make(map[cicdRunCorrelationPatchKey]struct{}, len(resolved))
	for _, stableKey := range requested {
		key, ok := resolved[stableKey]
		if !ok {
			return nil, fmt.Errorf(
				"load ci/cd run correlation patch: artifact tombstone key %q has no retained payload identity",
				stableKey,
			)
		}
		keys[key] = struct{}{}
	}
	return sortedCICDArtifactPatchRunKeys(keys), nil
}

func requireHistoricalCICDRuns(
	historical []facts.Envelope,
	keys []cicdRunCorrelationPatchKey,
) error {
	if len(keys) == 0 {
		return nil
	}
	available := make(map[cicdRunCorrelationPatchKey]struct{}, len(keys))
	for _, envelope := range historical {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		run, err := decodeCICDRun(envelope)
		if err != nil {
			continue
		}
		available[cicdRunCorrelationPatchKey{
			provider:   trimmedCICDField(run.Provider),
			runID:      trimmedCICDField(run.RunID),
			runAttempt: defaultCICDRunAttempt(trimmedCICDPtr(run.RunAttempt)),
		}] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := available[key]; !ok {
			return fmt.Errorf(
				"load ci/cd run correlation patch: artifact tombstone run %q has no retained ci.run identity",
				cicdRunCorrelationPatchKeyString(key),
			)
		}
	}
	return nil
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
