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
	return TargetScopeForComponent(item.ScopeID, item.AcceptanceUnitID, item.SourceRunID, item.GenerationID)
}

// TargetScopeForComponent derives the same privacy-safe scanner target from
// claim-carried identity instead of a workflow work item, for stdio
// components that receive an SDK claim over the v1alpha3 contract (Refs
// #8878). The locator hash is recomputed deterministically from the scope
// ID — never invented, never carried — so component output bears the
// identical provenance the in-tree worker commits. The host owns the claim;
// this derives target identity only.
func TargetScopeForComponent(scopeID, acceptanceUnitID, sourceRunID, generationID string) (TargetScope, error) {
	if strings.TrimSpace(scopeID) == "" {
		return TargetScope{}, fmt.Errorf("target scope_id must not be blank")
	}
	if strings.TrimSpace(acceptanceUnitID) == "" {
		return TargetScope{}, fmt.Errorf("target acceptance_unit_id must not be blank")
	}
	if strings.TrimSpace(sourceRunID) == "" {
		return TargetScope{}, fmt.Errorf("target source_run_id must not be blank")
	}
	if strings.TrimSpace(generationID) == "" {
		return TargetScope{}, fmt.Errorf("target generation_id must not be blank")
	}
	digest := sha256.Sum256([]byte(scopeID))
	return TargetScope{
		Kind:             targetKindForComponent(scopeID, acceptanceUnitID),
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		LocatorHash:      safeLocatorHashPrefix + hex.EncodeToString(digest[:]),
	}, nil
}

// targetKindForComponent infers the target kind from the claim scope and
// acceptance unit with the same prefix rules the work-item derivation uses.
func targetKindForComponent(scopeID, acceptanceUnitID string) TargetKind {
	for _, value := range []string{scopeID, acceptanceUnitID} {
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
