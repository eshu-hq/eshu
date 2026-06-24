// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const gibibyte int64 = 1 << 30

// DefaultResourceLimits returns the starting resource contract for one
// scanner-worker analyzer profile.
func DefaultResourceLimits(analyzer AnalyzerKind) (ResourceLimits, error) {
	switch analyzer {
	case AnalyzerSourceAnalysis, AnalyzerSecretScan, AnalyzerLicenseScan, AnalyzerMisconfigurationScan:
		return ResourceLimits{
			CPUMillis:     4000,
			MemoryBytes:   4 * gibibyte,
			Timeout:       10 * time.Minute,
			MaxInputBytes: 2 * gibibyte,
			MaxFiles:      250000,
			MaxFacts:      50000,
		}, nil
	case AnalyzerSBOMGeneration, AnalyzerOSPackageExtraction:
		return ResourceLimits{
			CPUMillis:     4000,
			MemoryBytes:   8 * gibibyte,
			Timeout:       10 * time.Minute,
			MaxInputBytes: 2 * gibibyte,
			MaxFiles:      250000,
			MaxFacts:      50000,
		}, nil
	case AnalyzerImageUnpacking:
		return ResourceLimits{
			CPUMillis:     6000,
			MemoryBytes:   12 * gibibyte,
			Timeout:       15 * time.Minute,
			MaxInputBytes: 4 * gibibyte,
			MaxFiles:      250000,
			MaxFacts:      50000,
		}, nil
	default:
		lane, ok := AnalyzerLane(analyzer)
		if ok {
			return ResourceLimits{}, fmt.Errorf("analyzer %q belongs to %q, not %q", analyzer, lane, LaneScannerWorker)
		}
		return ResourceLimits{}, fmt.Errorf("unknown analyzer %q", analyzer)
	}
}

// TargetScopeFromWorkItem derives a privacy-safe scanner target from a
// workflow work item. Repository is the legacy default; image and artifact are
// inferred from scanner-worker, source URI, or acceptance-unit prefixes. Raw
// scope IDs stay out of retry and dead-letter payloads.
func TargetScopeFromWorkItem(item workflow.WorkItem) (TargetScope, error) {
	if err := item.Validate(); err != nil {
		return TargetScope{}, err
	}
	digest := sha256.Sum256([]byte(item.ScopeID))
	return TargetScope{
		Kind:             targetKindFromWorkItem(item),
		ScopeID:          item.ScopeID,
		AcceptanceUnitID: item.AcceptanceUnitID,
		SourceRunID:      item.SourceRunID,
		GenerationID:     item.GenerationID,
		LocatorHash:      safeLocatorHashPrefix + hex.EncodeToString(digest[:]),
	}, nil
}

func targetKindFromWorkItem(item workflow.WorkItem) TargetKind {
	for _, value := range []string{item.ScopeID, item.AcceptanceUnitID} {
		if kind, ok := targetKindFromIdentity(value); ok {
			return kind
		}
	}
	return TargetRepository
}

func targetKindFromIdentity(value string) (TargetKind, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	switch {
	case strings.HasPrefix(trimmed, "scanner-worker://repository/"), strings.HasPrefix(trimmed, "repository:"):
		return TargetRepository, true
	case strings.HasPrefix(trimmed, "scanner-worker://image/"), strings.HasPrefix(trimmed, "image:"):
		return TargetImage, true
	case strings.HasPrefix(trimmed, "scanner-worker://artifact/"), strings.HasPrefix(trimmed, "artifact:"):
		return TargetArtifact, true
	default:
		return "", false
	}
}
