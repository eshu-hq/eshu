// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

func newUnsupportedWarning(
	input scannerworker.ClaimInput,
	target TargetConfig,
	snapshot Snapshot,
	observedAt time.Time,
) (facts.Envelope, error) {
	stableKey := facts.StableID(facts.ScannerWorkerWarningFactKind, map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"generation_id":       input.GenerationID,
		"reason":              warningReasonUnsupported,
	})
	payload, err := factschema.EncodeScannerWorkerWarning(scannerworkerv1.Warning{
		Analyzer:          string(input.Analyzer),
		TargetKind:        string(input.Target.Kind),
		TargetLocatorHash: input.Target.LocatorHash,
		Reason:            warningReasonUnsupported,
		WarningClass:      "scanner_worker_warning",
		AnalysisStatus:    analysisStatusNotScanned,
		CoverageStatus:    coverageStatusUnsupported,
		ImageReference:    strings.TrimSpace(target.ImageReference),
		ImageDigest:       strings.TrimSpace(target.ImageDigest),
		EvidenceSource:    evidenceSourceForWarning(target, snapshot),
		ExtractionReason:  firstNonBlank(snapshot.ExtractionReason, extractionUnsupported),
		Distro:            stringPtrIfPresent(string(firstNonBlankDistro(snapshot.Distro, target.Distro))),
		DistroVersion:     stringPtrIfPresent(firstNonBlank(snapshot.DistroVersion, target.DistroVersion)),
		PackageManager:    stringPtrIfPresent(string(firstNonBlankPackageManager(snapshot.PackageManager, target.PackageManager))),
	})
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID:           facts.ScannerWorkerWarningFactKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         facts.ScannerWorkerWarningFactKind,
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

func evidenceSourceForWarning(target TargetConfig, snapshot Snapshot) string {
	if snapshot.EvidenceSource != "" {
		return string(snapshot.EvidenceSource)
	}
	if strings.TrimSpace(target.RootFSPath) != "" {
		return string(EvidenceSourceRootFS)
	}
	return string(EvidenceSourceLayer)
}

func firstNonBlankPackageManager(
	values ...ospackagevulnerability.PackageManager,
) ospackagevulnerability.PackageManager {
	for _, value := range values {
		if strings.TrimSpace(string(value)) != "" {
			return value
		}
	}
	return ""
}
