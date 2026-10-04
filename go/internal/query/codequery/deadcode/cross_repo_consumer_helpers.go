// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func crossRepoDeadCodeHasStrongLiveEvidence(evidence []CrossRepoDeadCodeEvidence) bool {
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			continue
		}
		if item.Confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			return true
		}
	}
	return false
}

func crossRepoDeadCodeStrongestConfidenceLabel(evidence []CrossRepoDeadCodeEvidence) string {
	best := 0.0
	label := ""
	for _, item := range evidence {
		if item.Confidence > best {
			best = item.Confidence
			label = item.ConfidenceLabel
		}
	}
	if label == "" {
		return CrossRepoDeadCodeConfidenceLabel(best)
	}
	return label
}

// CrossRepoDeadCodeConfidenceLabel maps a numeric consumer-evidence confidence
// to its response label: high at 0.9 and above, medium above the repository
// unique-name confidence, low for any other positive value, and unknown for zero.
func CrossRepoDeadCodeConfidenceLabel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName):
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

func crossRepoDeadCodeConsumerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func cleanCrossRepoDeadCodeStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
