// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

const (
	analysisStatusCompleted   = "completed"
	analysisStatusNotScanned  = "not_scanned"
	coverageStatusScanned     = "scanned"
	coverageStatusUnsupported = "unsupported"
)

func newAnalysisFact(
	input scannerworker.ClaimInput,
	target TargetConfig,
	snapshot Snapshot,
	resultCount int,
	factCount int,
	observedAt time.Time,
) (facts.Envelope, error) {
	if observedAt.IsZero() {
		observedAt = input.ObservedAt.UTC()
	}
	stableKey := facts.StableID(facts.ScannerWorkerAnalysisFactKind, map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"generation_id":       input.GenerationID,
		"analysis_status":     analysisStatusCompleted,
		"image_digest":        strings.TrimSpace(snapshot.ImageDigest),
	})
	payload, err := factschema.EncodeScannerWorkerAnalysis(scannerworkerv1.Analysis{
		Analyzer:                 string(input.Analyzer),
		TargetKind:               string(input.Target.Kind),
		TargetLocatorHash:        input.Target.LocatorHash,
		AnalysisStatus:           analysisStatusCompleted,
		CoverageStatus:           coverageStatusScanned,
		ResultCount:              resultCount,
		FactCount:                factCount,
		ImageReference:           strings.TrimSpace(snapshot.ImageReference),
		ImageDigest:              strings.TrimSpace(snapshot.ImageDigest),
		EvidenceSource:           string(snapshot.EvidenceSource),
		ExtractionReason:         snapshot.ExtractionReason,
		Distro:                   stringPtrIfPresent(string(snapshot.Distro)),
		DistroVersion:            stringPtrIfPresent(snapshot.DistroVersion),
		PackageManager:           stringPtrIfPresent(string(snapshot.PackageManager)),
		ConfiguredImageReference: stringPtrIfPresent(strings.TrimSpace(target.ImageReference)),
	})
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID:           facts.ScannerWorkerAnalysisFactKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         facts.ScannerWorkerAnalysisFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ScannerWorkerSchemaVersionV1,
		CollectorKind:    string(scope.CollectorScannerWorker),
		FencingToken:     input.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      input.Target.ScopeID,
			GenerationID: input.GenerationID,
			FactKey:      stableKey,
		},
	}, nil
}
