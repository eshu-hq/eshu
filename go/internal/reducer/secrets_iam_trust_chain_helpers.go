// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func sortSecretsIAMReadModels(models *SecretsIAMTrustChainReadModels) {
	sort.Slice(models.IdentityTrustChains, func(i, j int) bool {
		return models.IdentityTrustChains[i].ChainID < models.IdentityTrustChains[j].ChainID
	})
	sort.Slice(models.PrivilegePostureObservations, func(i, j int) bool {
		return models.PrivilegePostureObservations[i].ObservationID < models.PrivilegePostureObservations[j].ObservationID
	})
	sort.Slice(models.SecretAccessPaths, func(i, j int) bool {
		return models.SecretAccessPaths[i].PathID < models.SecretAccessPaths[j].PathID
	})
	sort.Slice(models.PostureGaps, func(i, j int) bool {
		return models.PostureGaps[i].GapID < models.PostureGaps[j].GapID
	})
}

func secretsIAMID(kind string, parts ...string) string {
	return kind + ":" + facts.StableID(kind, map[string]any{"parts": parts})
}

func secretsIAMFingerprint(kind string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "sha256:" + facts.StableID("SecretsIAMReducerFingerprint", map[string]any{
		"kind":  kind,
		"value": value,
	})
}

func addByKey(index map[string][]facts.Envelope, key string, envelope facts.Envelope) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], envelope)
}

func factIDs(envelopes []facts.Envelope) []string {
	out := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		out = append(out, envelope.FactID)
	}
	return uniqueSortedStrings(out)
}

func secretsIAMContainsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func secretsIAMContainsLower(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func secretsIAMStateFromSourceState(sourceState string) SecretsIAMTrustChainState {
	switch strings.TrimSpace(sourceState) {
	case string(SecretsIAMTrustChainStateUnsupported):
		return SecretsIAMTrustChainStateUnsupported
	case string(SecretsIAMTrustChainStatePermissionHidden):
		return SecretsIAMTrustChainStatePermissionHidden
	case string(SecretsIAMTrustChainStateStale):
		return SecretsIAMTrustChainStateStale
	case string(SecretsIAMTrustChainStatePartial):
		return SecretsIAMTrustChainStatePartial
	default:
		return SecretsIAMTrustChainStatePartial
	}
}
