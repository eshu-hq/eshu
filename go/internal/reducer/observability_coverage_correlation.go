// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ObservabilityCoverageCorrelationOutcome names the reducer decision for one
// observability coverage candidate. The issue #391 outcomes mirror
// ServiceCatalogCorrelationOutcome (#390), and the Grafana-stack extension adds
// class-aware drift and permission-hidden states without promoting provider
// metadata into graph truth.
type ObservabilityCoverageCorrelationOutcome string

const (
	// ObservabilityCoverageExact means an observability object resolved to a
	// CloudResource target by a stable identity (ARN or bare resource id), so the
	// coverage is canonical truth, not provenance.
	ObservabilityCoverageExact ObservabilityCoverageCorrelationOutcome = "exact"
	// ObservabilityCoverageDerived means coverage is real but inferred through
	// deterministic normalization or a name-only anchor (such as an X-Ray service
	// name) rather than an exact resource identity.
	ObservabilityCoverageDerived ObservabilityCoverageCorrelationOutcome = "derived"
	// ObservabilityCoverageAmbiguous means an observability object's target
	// identity matched multiple active CloudResource nodes; no single target is
	// picked and candidates are recorded.
	ObservabilityCoverageAmbiguous ObservabilityCoverageCorrelationOutcome = "ambiguous"
	// ObservabilityCoverageUnresolved means the observability object is valid but
	// its target is not present as a CloudResource in this generation, or a
	// monitored resource has no resolving observability object (the coverage gap).
	ObservabilityCoverageUnresolved ObservabilityCoverageCorrelationOutcome = "unresolved"
	// ObservabilityCoverageStale means the observability object resolved only to a
	// tombstoned resource fact (a lingering alarm over a deleted resource — a real
	// drift signal).
	ObservabilityCoverageStale ObservabilityCoverageCorrelationOutcome = "stale"
	// ObservabilityCoverageRejected means the signal is too weak or unsafe to
	// promote, such as a metric-name-only alarm with no resolvable resource
	// dimension. Rejected decisions are suppressed, never promoted to covered.
	ObservabilityCoverageRejected ObservabilityCoverageCorrelationOutcome = "rejected"
	// ObservabilityCoverageDrifted means declared, applied, and observed
	// metadata disagree or a live provider source reports a manual resource.
	ObservabilityCoverageDrifted ObservabilityCoverageCorrelationOutcome = "drifted"
	// ObservabilityCoveragePermissionHidden means evidence exists but the source
	// reports that credentials or RBAC prevented reading the backing object.
	ObservabilityCoveragePermissionHidden ObservabilityCoverageCorrelationOutcome = "permission_hidden"
)

// ObservabilityCoverageCorrelationDecision records one bounded observability
// coverage decision: either a coverage edge candidate (observability object →
// monitored target) or a gap finding keyed on an uncovered target. Fields carry
// IDs and classifications only; no metric values or dashboard body JSON are ever
// ingested, so the "no health assertions from telemetry values" contract holds
// structurally.
type ObservabilityCoverageCorrelationDecision struct {
	Provider               string
	CoverageSignal         string
	ObservabilityObjectRef string
	ObservabilityUID       string
	TargetUID              string
	TargetServiceRef       string
	Outcome                ObservabilityCoverageCorrelationOutcome
	Reason                 string
	CoverageStatus         string
	ProvenanceOnly         bool
	ResolutionMode         string
	SourceClass            string
	SourceClasses          []string
	SourceKind             string
	SourceKinds            []string
	SourceOutcome          string
	SourceOutcomes         []string
	ResourceClass          string
	FreshnessState         string
	CandidateTargetUIDs    []string
	EvidenceFactIDs        []string
}

// ObservabilityCoverageCorrelationWrite carries decisions for durable
// publication for one scope generation.
type ObservabilityCoverageCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []ObservabilityCoverageCorrelationDecision
}

// ObservabilityCoverageCorrelationWriteResult summarizes durable coverage writes.
type ObservabilityCoverageCorrelationWriteResult struct {
	FactsWritten    int
	EvidenceSummary string
}

// ObservabilityCoverageCorrelationWriter persists reducer-owned observability
// coverage correlations. Implementations MUST be idempotent by the decision's
// stable identity so reducer retries and duplicate facts converge on one fact.
type ObservabilityCoverageCorrelationWriter interface {
	WriteObservabilityCoverageCorrelations(context.Context, ObservabilityCoverageCorrelationWrite) (ObservabilityCoverageCorrelationWriteResult, error)
}

// ObservabilityCoverageCorrelationHandler correlates which monitored
// CloudResource nodes or observability metadata sources have coverage evidence
// versus which are uncovered, emitting durable provenance-only reducer facts. It
// writes no graph edges; the separate materialization domain projects exact AWS
// COVERS edges after canonical node readiness.
type ObservabilityCoverageCorrelationHandler struct {
	FactLoader  FactLoader
	Writer      ObservabilityCoverageCorrelationWriter
	Instruments *telemetry.Instruments
}

// Handle executes one observability coverage correlation reducer intent. It
// loads the scope generation's AWS and observability source facts, builds a
// bounded in-memory coverage index, classifies each observability object or
// metadata identity into a bounded outcome plus gap findings for uncovered
// targets, and writes durable provenance-only facts.
func (h ObservabilityCoverageCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainObservabilityCoverageCorrelation {
		return Result{}, fmt.Errorf("observability_coverage_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("observability coverage correlation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("observability coverage correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, observabilityCoverageCorrelationFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load observability coverage correlation facts: %w", err)
	}

	decisions, quarantined, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource/aws_relationship fact (a
	// missing required identity field) is quarantined as a visible input_invalid
	// dead-letter — counter + structured error log — while coverage still
	// classifies from every valid fact.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainObservabilityCoverageCorrelation, intent.ScopeID, intent.GenerationID, quarantined)
	counts := observabilityCoverageCounts(decisions)
	writeResult, err := h.Writer.WriteObservabilityCoverageCorrelations(ctx, ObservabilityCoverageCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write observability coverage correlations: %w", err)
	}
	h.emitCounters(ctx, decisions)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainObservabilityCoverageCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: observabilityCoverageSummary(len(decisions), counts, writeResult.FactsWritten),
		CanonicalWrites: writeResult.FactsWritten,
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// emitCounters records the ObservabilityCoverageCorrelations counter dimensioned
// by domain, outcome, and coverage_signal so an operator can answer which
// bounded signal class is losing coverage at 3 AM.
func (h ObservabilityCoverageCorrelationHandler) emitCounters(
	ctx context.Context,
	decisions []ObservabilityCoverageCorrelationDecision,
) {
	if h.Instruments == nil || h.Instruments.ObservabilityCoverageCorrelations == nil {
		return
	}
	type signalOutcome struct {
		signal  string
		outcome ObservabilityCoverageCorrelationOutcome
	}
	counts := make(map[signalOutcome]int, len(decisions))
	for _, decision := range decisions {
		counts[signalOutcome{decision.CoverageSignal, decision.Outcome}]++
	}
	for key, count := range counts {
		h.Instruments.ObservabilityCoverageCorrelations.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainObservabilityCoverageCorrelation)),
			telemetry.AttrOutcome(string(key.outcome)),
			telemetry.AttrCoverageSignal(key.signal),
		))
	}
}

// BuildObservabilityCoverageDecisions classifies observability coverage without
// fabricating a covered edge from name coincidence or a metric-name-only signal.
// It is a pure function over fact envelopes (no I/O) so the outcome contract is
// table-test friendly. An aws_resource or aws_relationship fact missing a
// required identity field is skipped and returned in the []quarantinedFact slice
// (a per-fact input_invalid dead-letter) so the caller records it visibly while
// still classifying coverage from every valid fact, rather than aborting the
// whole intent or classifying against an empty-string node identity.
func BuildObservabilityCoverageDecisions(envelopes []facts.Envelope) ([]ObservabilityCoverageCorrelationDecision, []quarantinedFact, error) {
	index, quarantined, err := buildObservabilityCoverageIndex(envelopes)
	if err != nil {
		return nil, nil, err
	}
	decisions := classifyObservabilityCoverage(index)
	decisions = append(decisions, classifyObservabilityMetadataEvidence(envelopes)...)
	sortObservabilityCoverageDecisions(decisions)
	return decisions, quarantined, nil
}

func observabilityCoverageCorrelationFactKinds() []string {
	return []string{
		facts.AWSResourceFactKind,
		facts.AWSRelationshipFactKind,
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityDeclaredFolderFactKind,
		facts.ObservabilityDeclaredDashboardFactKind,
		facts.ObservabilityDeclaredDatasourceFactKind,
		facts.ObservabilityDeclaredAlertRuleFactKind,
		facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityDeclaredMetricRouteFactKind,
		facts.ObservabilityDeclaredLogRouteFactKind,
		facts.ObservabilityDeclaredTraceRouteFactKind,
		facts.ObservabilityAppliedResourceFactKind,
		facts.ObservabilityAppliedSyncStateFactKind,
		facts.ObservabilityObservedDashboardFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind,
		facts.ObservabilityObservedLogSignalFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
		facts.ObservabilityCoverageWarningFactKind,
	}
}

func observabilityCoverageOutcomes() []ObservabilityCoverageCorrelationOutcome {
	return []ObservabilityCoverageCorrelationOutcome{
		ObservabilityCoverageExact,
		ObservabilityCoverageDerived,
		ObservabilityCoverageAmbiguous,
		ObservabilityCoverageUnresolved,
		ObservabilityCoverageStale,
		ObservabilityCoverageRejected,
		ObservabilityCoverageDrifted,
		ObservabilityCoveragePermissionHidden,
	}
}

func observabilityCoverageCounts(
	decisions []ObservabilityCoverageCorrelationDecision,
) map[ObservabilityCoverageCorrelationOutcome]int {
	counts := make(map[ObservabilityCoverageCorrelationOutcome]int, len(observabilityCoverageOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func observabilityCoverageSummary(
	evaluated int,
	counts map[ObservabilityCoverageCorrelationOutcome]int,
	factsWritten int,
) string {
	return fmt.Sprintf(
		"observability coverage correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d stale=%d rejected=%d drifted=%d permission_hidden=%d facts_written=%d",
		evaluated,
		counts[ObservabilityCoverageExact],
		counts[ObservabilityCoverageDerived],
		counts[ObservabilityCoverageAmbiguous],
		counts[ObservabilityCoverageUnresolved],
		counts[ObservabilityCoverageStale],
		counts[ObservabilityCoverageRejected],
		counts[ObservabilityCoverageDrifted],
		counts[ObservabilityCoveragePermissionHidden],
		factsWritten,
	)
}
