// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Format identifies the SBOM serialization shape this analyzer emits.
const Format = "cyclonedx"

// SpecVersionDefault is the CycloneDX spec version recorded on emitted
// document facts when the Source does not name one.
const SpecVersionDefault = "1.6"

// ToolDefault is the bounded `name version` label recorded on emitted document
// facts when the Source does not name a generator.
const ToolDefault = "eshu_scanner_sbom_generator 0.1.0"

// ParseStatusGenerated marks a document fact produced by the bounded SBOM
// analyzer (as opposed to a parsed third-party document).
const ParseStatusGenerated = "generated"

// Warning reason vocabulary recorded in sbom.warning payloads.
const (
	WarningReasonMissingSubject           = "missing_subject"
	WarningReasonMalformedSubjectDigest   = "malformed_subject_digest"
	WarningReasonComponentMissingIdentity = "component_missing_identity"
	WarningReasonNoComponentsFound        = "no_components_found"
	WarningReasonLockfileMalformed        = "lockfile_malformed"
	WarningReasonLockfileUnsupported      = "lockfile_unsupported"
)

// ErrUnsupportedTarget signals that the runtime Source cannot scan the claim's
// target shape. The analyzer maps this to a terminal unsupported_target
// failure.
var ErrUnsupportedTarget = errors.New("sbomgenerator: unsupported target")

// ErrSourceUnavailable signals that the runtime Source could not read its
// inputs because of a transient dependency outage. The analyzer maps this to a
// retryable source_unavailable failure.
var ErrSourceUnavailable = errors.New("sbomgenerator: source unavailable")

// Source is the runtime-owned port that returns bounded SBOM inventory for one
// scanner-worker claim. The runtime enforces filesystem, archive, and SDK
// boundaries; the analyzer enforces source-fact shape, failure classification,
// and resource-budget checks.
type Source interface {
	Collect(ctx context.Context, input scannerworker.ClaimInput) (Inventory, error)
}

// Inventory is the bounded SBOM evidence the runtime returns for one claim.
type Inventory struct {
	// SubjectDigest is the canonical "sha256:<64 hex>" digest the analyzer
	// records on the emitted document fact. Empty triggers a missing_subject
	// warning.
	SubjectDigest string
	// SpecVersion is the CycloneDX spec version the runtime targeted (e.g.
	// "1.6"). Empty falls back to SpecVersionDefault.
	SpecVersion string
	// SourceTool is the bounded "<name> <version>" label of the generator the
	// runtime used. Empty falls back to ToolDefault.
	SourceTool string
	// FileCount is the number of input files the runtime read. The analyzer
	// compares it against ClaimInput.Limits.MaxFiles.
	FileCount int64
	// InputBytes is the total number of bytes the runtime read. The analyzer
	// compares it against ClaimInput.Limits.MaxInputBytes.
	InputBytes int64
	// Components is the bounded component list. The analyzer skips components
	// with no PURL and no name+version identity and records a warning fact.
	Components []Component
	// Warnings is bounded non-fatal evidence, such as malformed lockfiles that
	// could not produce components. The analyzer emits these as sbom.warning
	// facts without turning the claim into a clean result.
	Warnings []Warning
	// ResourceUsage carries runtime-measured CPU and memory usage for this
	// inventory read. Scanner-worker telemetry records it after validation.
	ResourceUsage scannerworker.ResourceUsage
}

// Component is one component the runtime extracted from the bounded inventory.
type Component struct {
	PURL             string
	Name             string
	Version          string
	Type             string
	BomRef           string
	Ecosystem        string
	EvidenceSource   string
	LockfilePath     string
	DependencyScope  string
	DependencyType   string
	ExtractionReason string
}

// Warning is one non-fatal analyzer warning with bounded source evidence.
type Warning struct {
	Reason           string
	Summary          string
	Ecosystem        string
	EvidenceSource   string
	LockfilePath     string
	ExtractionReason string
}

// Analyzer is the bounded scanner-worker analyzer that converts one Source's
// Inventory into CycloneDX-compatible source facts. Construct one per claim or
// per worker (Analyzer is safe to reuse across claims when the Source is
// claim-scoped).
type Analyzer struct {
	// Source returns bounded inventory for one claim. Required.
	Source Source
	// Now overrides the analyzer clock for deterministic tests; defaults to
	// time.Now().UTC().
	Now func() time.Time
}

// Analyze implements scannerworker.Analyzer. It enforces resource limits,
// derives source facts from the Source's Inventory, and translates any
// runtime failure into a bounded scanner-worker failure.
func (a Analyzer) Analyze(ctx context.Context, input scannerworker.ClaimInput) (scannerworker.AnalyzerResult, error) {
	if a.Source == nil {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassAnalyzerFailed,
			scannerworker.ResourceUsage{},
			errors.New("sbomgenerator: source is required"),
		)
	}

	inventory, err := a.Source.Collect(ctx, input)
	if err != nil {
		return scannerworker.AnalyzerResult{}, classifySourceError(err)
	}

	if inventory.FileCount > input.Limits.MaxFiles {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassFileLimitExceeded,
			scannerworker.ResourceUsage{},
			fmt.Errorf("sbomgenerator: file count %d exceeds limit %d", inventory.FileCount, input.Limits.MaxFiles),
		)
	}
	if inventory.InputBytes > input.Limits.MaxInputBytes {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassInputLimitExceeded,
			scannerworker.ResourceUsage{},
			fmt.Errorf("sbomgenerator: input bytes %d exceeds limit %d", inventory.InputBytes, input.Limits.MaxInputBytes),
		)
	}

	_, subjectWarning := normalizeSubjectDigest(inventory.SubjectDigest)
	precheckedFacts := 1 + emittedInventoryWarningCount(inventory.Warnings)
	if subjectWarning != "" {
		precheckedFacts++
	}
	if len(inventory.Components) == 0 {
		precheckedFacts++
	}
	// Pre-check facts that do not depend on component aggregation. Component
	// limits are checked while building facts so repeated malformed components
	// can collapse into one warning row instead of dead-lettering solely
	// because the raw occurrence count is high.
	if precheckedFacts > input.Limits.MaxFacts {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassFactLimitExceeded,
			scannerworker.ResourceUsage{},
			fmt.Errorf("sbomgenerator: inventory would emit at least %d facts above max_facts %d", precheckedFacts, input.Limits.MaxFacts),
		)
	}

	envelopes, factCount, componentCount, err := a.buildFacts(input, inventory)
	if err != nil {
		return scannerworker.AnalyzerResult{}, err
	}
	if factCount > input.Limits.MaxFacts {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassFactLimitExceeded,
			scannerworker.ResourceUsage{},
			fmt.Errorf("sbomgenerator: emitted %d facts above max_facts %d", factCount, input.Limits.MaxFacts),
		)
	}

	return scannerworker.AnalyzerResult{
		Output: scannerworker.FactOutput{
			TargetCount: 1,
			ResultCount: componentCount,
			Facts:       envelopes,
		},
		Usage: inventory.ResourceUsage,
	}, nil
}

func classifySourceError(err error) error {
	switch {
	case errors.Is(err, ErrUnsupportedTarget):
		return scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassUnsupportedTarget,
			scannerworker.ResourceUsage{},
			err,
		)
	case errors.Is(err, ErrSourceUnavailable):
		return scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassSourceUnavailable,
			scannerworker.ResourceUsage{},
			err,
		)
	}
	var existing scannerworker.AnalyzerFailure
	if errors.As(err, &existing) {
		return err
	}
	// Privacy: the wrapped cause may contain raw repository paths or image
	// names. NewTerminalAnalyzerFailure discards the cause so workflow payloads
	// stay bounded; the analyzer's own Error() string carries only the failure
	// class.
	return scannerworker.NewTerminalAnalyzerFailure(
		scannerworker.FailureClassAnalyzerFailed,
		scannerworker.ResourceUsage{},
		nil,
	)
}

func (a Analyzer) buildFacts(input scannerworker.ClaimInput, inventory Inventory) ([]facts.Envelope, int, int, error) {
	observedAt := a.observedAt(input)
	subjectDigest, subjectWarning := normalizeSubjectDigest(inventory.SubjectDigest)
	specVersion := inventory.SpecVersion
	if specVersion == "" {
		specVersion = SpecVersionDefault
	}
	tool := inventory.SourceTool
	if tool == "" {
		tool = ToolDefault
	}
	subjectWarningFacts := 0
	if subjectWarning != "" {
		subjectWarningFacts = 1
	}
	inventoryWarningFacts := emittedInventoryWarningCount(inventory.Warnings)

	documentID := newDocumentID(input, subjectDigest, observedAt)

	componentFacts := make([]facts.Envelope, 0, len(inventory.Components))
	warningFacts := make([]facts.Envelope, 0)
	componentWarnings := newComponentWarningAggregator()
	usedIdentities := make(map[string]struct{}, len(inventory.Components))
	emittedComponents := 0

	for idx, comp := range inventory.Components {
		identity := componentIdentity(comp)
		if identity == "" {
			componentWarnings.addMissingIdentity(idx, comp, tool)
			if componentFactUpperBound(len(componentFacts), componentWarnings.factCount(), inventoryWarningFacts, subjectWarningFacts) > input.Limits.MaxFacts {
				return nil, 0, 0, scannerworker.NewTerminalAnalyzerFailure(
					scannerworker.FailureClassFactLimitExceeded,
					scannerworker.ResourceUsage{},
					fmt.Errorf("sbomgenerator: inventory would emit more than max_facts %d", input.Limits.MaxFacts),
				)
			}
			continue
		}
		fact, ok := newComponentFact(input, observedAt, documentID, comp, identity, usedIdentities)
		if !ok {
			componentWarnings.addDuplicateIdentity(idx, comp, identity, tool)
			if componentFactUpperBound(len(componentFacts), componentWarnings.factCount(), inventoryWarningFacts, subjectWarningFacts) > input.Limits.MaxFacts {
				return nil, 0, 0, scannerworker.NewTerminalAnalyzerFailure(
					scannerworker.FailureClassFactLimitExceeded,
					scannerworker.ResourceUsage{},
					fmt.Errorf("sbomgenerator: inventory would emit more than max_facts %d", input.Limits.MaxFacts),
				)
			}
			continue
		}
		componentFacts = append(componentFacts, fact)
		emittedComponents++
		if componentFactUpperBound(len(componentFacts), componentWarnings.factCount(), inventoryWarningFacts, subjectWarningFacts) > input.Limits.MaxFacts {
			return nil, 0, 0, scannerworker.NewTerminalAnalyzerFailure(
				scannerworker.FailureClassFactLimitExceeded,
				scannerworker.ResourceUsage{},
				fmt.Errorf("sbomgenerator: inventory would emit more than max_facts %d", input.Limits.MaxFacts),
			)
		}
	}

	warningFacts = append(warningFacts, componentWarnings.facts(input, observedAt, documentID)...)
	for _, warning := range inventory.Warnings {
		if strings.TrimSpace(warning.Reason) == "" {
			continue
		}
		warningFacts = append(warningFacts, newWarningFactWithEvidence(input, observedAt, documentID, warning))
	}
	if subjectWarning != "" {
		warningFacts = append(warningFacts, newWarningFact(input, observedAt, documentID, subjectWarning, subjectWarningSummary(subjectWarning, inventory.SubjectDigest)))
	}
	if emittedComponents == 0 {
		warningFacts = append(warningFacts, newWarningFact(input, observedAt, documentID, WarningReasonNoComponentsFound, "sbom generator returned zero usable components"))
	}

	docFact := newDocumentFact(input, observedAt, documentInput{
		documentID:     documentID,
		subjectDigest:  subjectDigest,
		specVersion:    specVersion,
		tool:           tool,
		componentCount: len(componentFacts),
		warningCount:   len(warningFacts),
	})

	all := make([]facts.Envelope, 0, 1+len(componentFacts)+len(warningFacts))
	all = append(all, docFact)
	all = append(all, componentFacts...)
	all = append(all, warningFacts...)
	return all, len(all), emittedComponents, nil
}

func emittedInventoryWarningCount(warnings []Warning) int {
	count := 0
	for _, warning := range warnings {
		if strings.TrimSpace(warning.Reason) != "" {
			count++
		}
	}
	return count
}

func componentFactUpperBound(componentFacts int, componentWarningFacts int, inventoryWarnings int, subjectWarningFacts int) int {
	noComponentsWarningFacts := 0
	if componentFacts == 0 {
		noComponentsWarningFacts = 1
	}
	return 1 + componentFacts + componentWarningFacts + inventoryWarnings + subjectWarningFacts + noComponentsWarningFacts
}

func (a Analyzer) observedAt(input scannerworker.ClaimInput) time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	if !input.ObservedAt.IsZero() {
		return input.ObservedAt
	}
	return time.Now().UTC()
}
