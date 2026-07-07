// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	warningReasonUnsupported = "image_analyzer_unsupported_target"
	extractionUnsupported    = "unsupported_or_missing_package_database"
	extractionMissingDigest  = "missing_image_digest"
	extractionMalformedImage = "malformed_image_evidence"
	extractionOK             = "package_database_extracted"
)

var (
	errTargetUnavailable  = errors.New("image analyzer target unavailable")
	errSourceUnavailable  = errors.New("image analyzer source unavailable")
	errInputLimitExceeded = errors.New("image analyzer input limit exceeded")
	errFileLimitExceeded  = errors.New("image analyzer file limit exceeded")
	errFactLimitExceeded  = errors.New("image analyzer fact limit exceeded")
	errUnsupportedTarget  = errors.New("image analyzer unsupported target")
)

// Analyzer extracts installed package source facts from configured image
// rootfs or local OCI layer evidence.
type Analyzer struct {
	collectorInstanceID string
	targets             map[string]TargetConfig
	now                 func() time.Time
}

// NewAnalyzer validates configuration and builds an image unpacking analyzer.
func NewAnalyzer(config AnalyzerConfig) (*Analyzer, error) {
	collectorInstanceID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorInstanceID == "" {
		return nil, fmt.Errorf("collector instance ID is required")
	}
	targets := make(map[string]TargetConfig, len(config.Targets))
	for i, target := range config.Targets {
		validated, err := target.validate()
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		if _, exists := targets[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate image analyzer target scope_id %q", validated.ScopeID)
		}
		targets[validated.ScopeID] = validated
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one image analyzer target is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Analyzer{
		collectorInstanceID: collectorInstanceID,
		targets:             targets,
		now:                 now,
	}, nil
}

// Analyze reads one claimed image target and emits installed package source
// facts or explicit unsupported warning evidence.
func (a *Analyzer) Analyze(
	ctx context.Context,
	input scannerworker.ClaimInput,
) (scannerworker.AnalyzerResult, error) {
	target, ok := a.targets[strings.TrimSpace(input.Target.ScopeID)]
	if !ok {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassUnsupportedTarget,
			scannerworker.ResourceUsage{},
			errUnsupportedTarget,
		)
	}
	snapshot, usage, err := a.snapshot(ctx, target, input.Limits)
	if err != nil {
		if errors.Is(err, errUnsupportedTarget) {
			return a.unsupportedResult(input, target, snapshot, usage)
		}
		return scannerworker.AnalyzerResult{}, analyzerFailure(err, usage)
	}
	if strings.TrimSpace(snapshot.ImageDigest) == "" {
		snapshot.ExtractionReason = extractionMissingDigest
		return a.unsupportedResult(input, target, snapshot, usage)
	}
	envelopes, resultCount, err := a.envelopes(input, snapshot)
	if err != nil {
		if errors.Is(err, errUnsupportedTarget) {
			return a.unsupportedResult(input, target, snapshot, usage)
		}
		return scannerworker.AnalyzerResult{}, analyzerFailure(err, usage)
	}
	if len(envelopes) > input.Limits.MaxFacts {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassFactLimitExceeded,
			usage,
			errFactLimitExceeded,
		)
	}
	analysis, err := newAnalysisFact(input, target, snapshot, resultCount, len(envelopes)+1, a.now().UTC())
	if err != nil {
		return scannerworker.AnalyzerResult{}, analyzerFailure(err, usage)
	}
	envelopes = append([]facts.Envelope{analysis}, envelopes...)
	if len(envelopes) > input.Limits.MaxFacts {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassFactLimitExceeded,
			usage,
			errFactLimitExceeded,
		)
	}
	return scannerworker.AnalyzerResult{
		Output: scannerworker.FactOutput{
			TargetCount: 1,
			ResultCount: resultCount,
			Facts:       envelopes,
		},
		Usage: usage,
	}, nil
}

func (a *Analyzer) snapshot(
	ctx context.Context,
	target TargetConfig,
	limits scannerworker.ResourceLimits,
) (Snapshot, scannerworker.ResourceUsage, error) {
	if target.RootFSPath != "" {
		provider := osruntime.LocalRootFSProvider{}
		snapshot, usage, err := provider.Snapshot(ctx, osruntime.TargetConfig{
			ScopeID:            target.ScopeID,
			RootFSPath:         target.RootFSPath,
			SourceURI:          target.SourceURI,
			SourceRecordID:     target.SourceRecordID,
			Distro:             target.Distro,
			DistroVersion:      target.DistroVersion,
			PackageManager:     target.PackageManager,
			Repositories:       target.Repositories,
			ThirdPartyPackages: target.ThirdPartyPackages,
		}, limits)
		if err != nil {
			translated := translateOSRuntimeError(err)
			if errors.Is(translated, errTargetUnavailable) {
				if unsupported, unsupportedUsage, ok := readRootFSUnsupportedSnapshot(ctx, target, limits, a.now); ok {
					return unsupported, mergeUsage(usage, unsupportedUsage), errUnsupportedTarget
				}
			}
			return Snapshot{}, usage, translated
		}
		return Snapshot{
			Distro:             snapshot.Distro,
			DistroVersion:      snapshot.DistroVersion,
			PackageManager:     snapshot.PackageManager,
			Repositories:       snapshot.Repositories,
			InstalledDB:        snapshot.InstalledDB,
			Status:             snapshot.Status,
			ThirdPartyPackages: snapshot.ThirdPartyPackages,
			SourceURI:          firstNonBlank(snapshot.SourceURI, target.SourceURI),
			SourceRecordID:     firstNonBlank(snapshot.SourceRecordID, target.SourceRecordID),
			ImageReference:     target.ImageReference,
			ImageDigest:        target.ImageDigest,
			EvidenceSource:     EvidenceSourceRootFS,
			ExtractionReason:   extractionOK,
			ObservedAt:         snapshot.ObservedAt,
		}, usage, nil
	}
	return readLayerSnapshot(ctx, target, limits, a.now)
}

func mergeUsage(primary scannerworker.ResourceUsage, secondary scannerworker.ResourceUsage) scannerworker.ResourceUsage {
	if secondary.CPUSeconds > primary.CPUSeconds {
		primary.CPUSeconds = secondary.CPUSeconds
	}
	if secondary.PeakMemoryBytes > primary.PeakMemoryBytes {
		primary.PeakMemoryBytes = secondary.PeakMemoryBytes
	}
	return primary
}

func (a *Analyzer) envelopes(input scannerworker.ClaimInput, snapshot Snapshot) ([]facts.Envelope, int, error) {
	observedAt := snapshot.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = input.ObservedAt.UTC()
	}
	if observedAt.IsZero() {
		observedAt = a.now().UTC()
	}
	ctx := ospackagevulnerability.EnvelopeContext{
		ScopeID:             input.Target.ScopeID,
		GenerationID:        input.GenerationID,
		CollectorInstanceID: a.collectorInstanceID,
		FencingToken:        input.FencingToken,
		ObservedAt:          observedAt,
		SourceURI:           snapshot.SourceURI,
		SourceRecordID:      firstNonBlank(snapshot.SourceRecordID, snapshot.ImageDigest),
	}
	var packages []ospackagevulnerability.OSPackage
	var warnings []ospackagevulnerability.OSPackageWarning
	var err error
	switch snapshot.PackageManager {
	case ospackagevulnerability.PackageManagerAPK:
		packages, warnings, err = ospackagevulnerability.ParseAlpine(ospackagevulnerability.AlpineSnapshot{
			DistroVersion: snapshot.DistroVersion,
			Repositories:  snapshot.Repositories,
			InstalledDB:   snapshot.InstalledDB,
		})
	case ospackagevulnerability.PackageManagerDPKG:
		packages, warnings, err = ospackagevulnerability.ParseDebian(ospackagevulnerability.DebianSnapshot{
			DistroVersion:      snapshot.DistroVersion,
			Repositories:       snapshot.Repositories,
			Status:             snapshot.Status,
			ThirdPartyPackages: snapshot.ThirdPartyPackages,
		})
	default:
		return nil, 0, fmt.Errorf("%w: unsupported package database", errUnsupportedTarget)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errUnsupportedTarget, err)
	}
	envelopes, err := ospackagevulnerability.BuildEnvelopes(ctx, packages, warnings)
	if err != nil {
		return nil, 0, err
	}
	for i := range envelopes {
		addImageEvidence(envelopes[i].Payload, snapshot)
	}
	return envelopes, len(packages), nil
}

func (a *Analyzer) unsupportedResult(
	input scannerworker.ClaimInput,
	target TargetConfig,
	snapshot Snapshot,
	usage scannerworker.ResourceUsage,
) (scannerworker.AnalyzerResult, error) {
	fact, err := newUnsupportedWarning(input, target, snapshot, a.now().UTC())
	if err != nil {
		return scannerworker.AnalyzerResult{}, analyzerFailure(err, usage)
	}
	return scannerworker.AnalyzerResult{
		Output: scannerworker.FactOutput{
			TargetCount: 1,
			ResultCount: 0,
			Facts:       []facts.Envelope{fact},
		},
		Usage: usage,
	}, nil
}

func addImageEvidence(payload map[string]any, snapshot Snapshot) {
	payload["image_reference"] = snapshot.ImageReference
	payload["image_digest"] = snapshot.ImageDigest
	payload["evidence_source"] = string(snapshot.EvidenceSource)
	payload["extraction_reason"] = snapshot.ExtractionReason
}

func stringPtrIfPresent(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// stringPtrAlways returns a non-nil pointer to value so an optional contract
// field is emitted even when value is empty. The image analyzer uses it for the
// warning's image_reference, image_digest, evidence_source, and extraction_reason
// so its wire shape stays byte-identical to the pre-contract required-string
// payload: those keys are always present (image_reference/image_digest may be
// empty for a validated rootfs/layer-only target that carries no image
// identity). Only the non-image WarningAnalyzer fallback omits them, via nil.
func stringPtrAlways(value string) *string {
	return &value
}

func analyzerFailure(err error, usage scannerworker.ResourceUsage) error {
	switch {
	case errors.Is(err, errTargetUnavailable):
		return scannerworker.NewRetryableAnalyzerFailure(scannerworker.FailureClassTargetUnavailable, usage, err)
	case errors.Is(err, errSourceUnavailable):
		return scannerworker.NewRetryableAnalyzerFailure(scannerworker.FailureClassSourceUnavailable, usage, err)
	case errors.Is(err, errInputLimitExceeded):
		return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassInputLimitExceeded, usage, err)
	case errors.Is(err, errFileLimitExceeded):
		return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassFileLimitExceeded, usage, err)
	case errors.Is(err, errFactLimitExceeded):
		return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassFactLimitExceeded, usage, err)
	case errors.Is(err, errUnsupportedTarget):
		return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassUnsupportedTarget, usage, err)
	default:
		return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassAnalyzerFailed, usage, err)
	}
}

func translateOSRuntimeError(err error) error {
	switch {
	case errors.Is(err, osruntime.ErrTargetUnavailable):
		return fmt.Errorf("%w: rootfs target unavailable", errTargetUnavailable)
	case errors.Is(err, osruntime.ErrSourceUnavailable):
		return fmt.Errorf("%w: rootfs source unavailable", errSourceUnavailable)
	case errors.Is(err, osruntime.ErrInputLimitExceeded):
		return fmt.Errorf("%w: rootfs input limit exceeded", errInputLimitExceeded)
	case errors.Is(err, osruntime.ErrFileLimitExceeded):
		return fmt.Errorf("%w: rootfs file limit exceeded", errFileLimitExceeded)
	case errors.Is(err, osruntime.ErrFactLimitExceeded):
		return fmt.Errorf("%w: rootfs fact limit exceeded", errFactLimitExceeded)
	case errors.Is(err, osruntime.ErrUnsupportedTarget):
		return fmt.Errorf("%w: rootfs unsupported", errUnsupportedTarget)
	default:
		return err
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
