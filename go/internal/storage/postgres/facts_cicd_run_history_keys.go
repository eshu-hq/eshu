// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"sort"
	"strings"
)

func cleanCICDArtifactTombstoneKeys(keys []string) []string {
	unique := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key = strings.TrimSpace(key); key != "" {
			unique[key] = struct{}{}
		}
	}
	cleaned := make([]string, 0, len(unique))
	for key := range unique {
		cleaned = append(cleaned, key)
	}
	sort.Strings(cleaned)
	return cleaned
}

type cicdRunHistoryKey struct {
	provider   string
	runID      string
	runAttempt string
}

func cleanCICDRunHistoryKeys(providers, runIDs, runAttempts []string) ([]cicdRunHistoryKey, error) {
	if len(providers) != len(runIDs) || len(providers) != len(runAttempts) {
		return nil, fmt.Errorf("ci/cd run history key columns must have equal lengths")
	}
	unique := make(map[cicdRunHistoryKey]struct{}, len(providers))
	for index := range providers {
		key := cicdRunHistoryKey{
			provider:   strings.TrimSpace(providers[index]),
			runID:      strings.TrimSpace(runIDs[index]),
			runAttempt: strings.TrimSpace(runAttempts[index]),
		}
		if key.runAttempt == "" {
			key.runAttempt = "1"
		}
		if key.provider == "" || key.runID == "" {
			continue
		}
		unique[key] = struct{}{}
	}
	keys := make([]cicdRunHistoryKey, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := keys[i].provider + "\x00" + keys[i].runID + "\x00" + keys[i].runAttempt
		right := keys[j].provider + "\x00" + keys[j].runID + "\x00" + keys[j].runAttempt
		return left < right
	})
	return keys, nil
}

func splitCICDRunHistoryKeys(keys []cicdRunHistoryKey) ([]string, []string, []string) {
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
