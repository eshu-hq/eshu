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

// excludeSupersededCICDArtifacts keeps current live artifact snapshots
// authoritative by run key and artifact tombstones authoritative by stable
// identity. Retained run, environment, trigger, step, workflow-image, and
// deployment evidence still participates in the recomputation.
func excludeSupersededCICDArtifacts(
	historical []facts.Envelope,
	currentKeys []cicdRunCorrelationPatchKey,
	tombstoneStableKeys []string,
) []facts.Envelope {
	currentKeySet := make(map[cicdRunCorrelationPatchKey]struct{}, len(currentKeys))
	for _, key := range currentKeys {
		currentKeySet[key] = struct{}{}
	}
	tombstoneKeySet := make(map[string]struct{}, len(tombstoneStableKeys))
	for _, stableKey := range tombstoneStableKeys {
		tombstoneKeySet[stableKey] = struct{}{}
	}
	filtered := make([]facts.Envelope, 0, len(historical))
	for _, envelope := range historical {
		if envelope.FactKind == facts.CICDArtifactFactKind {
			if _, retired := tombstoneKeySet[strings.TrimSpace(envelope.StableFactKey)]; retired {
				continue
			}
		}
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if ok {
			if _, superseded := currentKeySet[key]; superseded {
				continue
			}
		}
		filtered = append(filtered, envelope)
	}
	return filtered
}

func excludeCICDArtifactTombstones(envelopes []facts.Envelope) []facts.Envelope {
	filtered := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.CICDArtifactFactKind && envelope.IsTombstone {
			continue
		}
		filtered = append(filtered, envelope)
	}
	return filtered
}
