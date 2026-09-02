// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CICDRunCorrelationOutcome names the reducer decision for one CI/CD run.
type CICDRunCorrelationOutcome string

const (
	// CICDRunCorrelationExact means the run has an explicit artifact digest
	// that joins to exactly one reducer-owned container-image identity row.
	CICDRunCorrelationExact CICDRunCorrelationOutcome = "exact"
	// CICDRunCorrelationDerived means the run has bounded provider evidence,
	// but not enough artifact identity to claim a canonical target.
	CICDRunCorrelationDerived CICDRunCorrelationOutcome = "derived"
	// CICDRunCorrelationAmbiguous means the run's artifact evidence matches
	// multiple possible canonical targets.
	CICDRunCorrelationAmbiguous CICDRunCorrelationOutcome = "ambiguous"
	// CICDRunCorrelationUnresolved means the run is valid evidence but lacks
	// the repository/commit anchors required for correlation.
	CICDRunCorrelationUnresolved CICDRunCorrelationOutcome = "unresolved"
	// CICDRunCorrelationRejected means the run only offered unsafe evidence,
	// such as shell text that hints at deployment without an artifact anchor.
	CICDRunCorrelationRejected CICDRunCorrelationOutcome = "rejected"
)

// CICDRunCorrelationDecision records the bounded reducer decision for one run.
type CICDRunCorrelationDecision struct {
	Provider     string
	RunID        string
	RunAttempt   string
	RepositoryID string
	CommitSHA    string
	Environment  string
	// EnvironmentEvidence names which evidence Environment came from:
	// "deploy_event" when an attached ci.deployment_event won selection
	// (classifyCICDDeploymentEventEnvironment), "declared" when the existing
	// ci.environment_observation path supplied it, and "" when the run has no
	// environment evidence at all. Issue #5426 branches on this value, so it
	// is published on the durable payload (cicdRunCorrelationPayload), not
	// kept in-memory only.
	EnvironmentEvidence string
	ArtifactDigest      string
	ImageRef            string
	Outcome             CICDRunCorrelationOutcome
	Reason              string
	ProvenanceOnly      bool
	CanonicalWrites     int
	EvidenceFactIDs     []string
	CanonicalTarget     string
	CorrelationKind     string
	SourceLayerKinds    []string
}

// CICDRunCorrelationWrite carries decisions for durable publication.
type CICDRunCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []CICDRunCorrelationDecision
}

// CICDRunCorrelationWriteResult summarizes durable CI/CD correlation writes.
type CICDRunCorrelationWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// CICDRunCorrelationWriter persists reducer-owned CI/CD run correlations.
type CICDRunCorrelationWriter interface {
	WriteCICDRunCorrelations(context.Context, CICDRunCorrelationWrite) (CICDRunCorrelationWriteResult, error)
}

// CICDRunCorrelationHandler joins CI/CD run facts with reducer-owned artifact
// identity evidence and publishes one durable decision per provider run.
type CICDRunCorrelationHandler struct {
	FactLoader  factload.FactLoader
	Writer      CICDRunCorrelationWriter
	Instruments *telemetry.Instruments
	// ProvenanceEdgeWriter projects exact workflow-image correlations into
	// independently owned BUILT_FROM graph assertions when configured.
	ProvenanceEdgeWriter reducercontract.ContainerImageProvenanceEdgeWriter
	// ProducerReadiness is the #5709 cross-scope correctness floor. This domain
	// reads container_image_identity output published by a DIFFERENT ingestion
	// scope, so a correlation that runs before that scope's generation is
	// activated resolves nothing and would otherwise write a durable "no
	// answer" that no later event disturbs. When wired, an empty cross-scope
	// join whose producer scopes have not activated defers instead. Optional:
	// nil keeps the pre-#5709 behaviour.
	ProducerReadiness crossscope.ProducerReadiness
	// Logger records a cross-scope readiness deferral as its own structured
	// line. Optional: nil silences it. Worth wiring -- the deferral's failure
	// class freezes attempt_count, so the queue row alone cannot tell an
	// operator how long this consumer has been waiting.
	Logger *slog.Logger
}

// Handle executes one CI/CD run correlation reducer intent.
func (h CICDRunCorrelationHandler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainCICDRunCorrelation {
		return reducercontract.Result{}, fmt.Errorf("ci_cd_run_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("ci/cd run correlation fact loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("ci/cd run correlation writer is required")
	}

	envelopes, err := factload.LoadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, cicdRunCorrelationFactKinds())
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load ci/cd run correlation facts: %w", err)
	}
	envelopes, patchGeneration, err := h.loadCICDRunCorrelationPatchFacts(ctx, intent, envelopes)
	if err != nil {
		return reducercontract.Result{}, err
	}
	workflowBridgeLoadStarted := time.Now()
	workflowImages, err := h.loadActiveCICDWorkflowImageFacts(ctx, cicdRunRepositoryIDs(envelopes))
	workflowBridgeLoadDuration := time.Since(workflowBridgeLoadStarted)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active git workflow image facts: %w", err)
	}
	envelopes = append(envelopes, workflowImages...)

	// The cross-scope floor (#5709) sits on the load that reads the producer's
	// output, not at admission: producer readiness is not knowable when this
	// intent is enqueued.
	//
	// The readiness signal is sampled BEFORE the load and combined with the
	// post-load count afterwards. Sampling it after would let a producer
	// generation activate in between, so the store would report "ready" about
	// a snapshot the load had already taken without it, and the handler would
	// durably write an empty correlation that no later event repairs -- the
	// bug this floor exists to prevent, through a narrower window. One clock
	// reading is threaded through both halves so a slow load cannot push the
	// intent past the elapsed bound by itself.
	digests := ciArtifactDigests(envelopes)
	imageRefs := ciWorkflowImageRefs(envelopes)
	// Resolved once and handed to both halves, so the floor and the load cannot
	// disagree about whether a cross-scope lookup happened. A nil loader means
	// this pass asks nothing, which is not a producer readiness miss.
	identityLoader := h.crossScopeIdentityLookup(digests, imageRefs)
	readinessSampledAt := time.Now()
	readinessSignal, err := crossscope.CheckProducerReadinessBeforeLoad(
		ctx, h.ProducerReadiness, intent, readinessSampledAt,
		identityLoader != nil,
	)
	if err != nil {
		return reducercontract.Result{}, err
	}
	active, err := loadActiveCICDRunCorrelationFacts(ctx, identityLoader, digests, imageRefs)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load active ci/cd artifact identity facts: %w", err)
	}
	// Returned unwrapped so the queue reads the non-counting failure class off
	// it (#5709).
	resolvedByProducer := crossscope.SingleProducerResolvedCounts(readinessSignal.ProducerDomains, len(active))
	if unready := crossscope.UnreadyProducers(readinessSignal, resolvedByProducer); len(unready) > 0 {
		crossscope.LogProducerNotReadyDefer(ctx, h.Logger, intent, readinessSampledAt, unready)
		return reducercontract.Result{}, crossscope.NewProducerNotReadyError(
			intent.Domain, intent.ScopeID, intent.GenerationID, unready,
		)
	}
	envelopes = append(envelopes, active...)

	decisions, quarantined, deploymentEventsSkipped, err := buildCICDRunCorrelationDecisionsWithQuarantine(envelopes)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("build ci/cd run correlation decisions: %w", err)
	}
	evaluatedCount := len(decisions)
	preservedCount := 0
	if patchGeneration && len(decisions) > maxCICDRunCorrelationPatchDecisions {
		return reducercontract.Result{}, fmt.Errorf(
			"rebuild ci/cd run correlation patch: decisions exceed safety cap %d",
			maxCICDRunCorrelationPatchDecisions,
		)
	}
	counts := cicdRunCorrelationCounts(decisions)
	writeResult, err := h.Writer.WriteCICDRunCorrelations(ctx, CICDRunCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("write ci/cd run correlations: %w", err)
	}
	if err := h.ProjectCICDWorkflowImageBuiltFromEdges(ctx, intent, decisions); err != nil {
		return reducercontract.Result{}, err
	}
	h.emitCounters(ctx, counts)
	h.emitDeploymentEventSkips(ctx, deploymentEventsSkipped)
	quarantinedCount := factdecode.RecordQuarantinedFacts(ctx, h.Instruments, reducercontract.DomainCICDRunCorrelation, intent.ScopeID, intent.GenerationID, quarantined)

	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainCICDRunCorrelation,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: cicdRunCorrelationSummary(evaluatedCount, preservedCount, counts, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
		SubSignals:      factdecode.InputInvalidSubSignals(quarantinedCount),
		SubDurations: map[string]float64{
			"workflow_image_bridge_load": workflowBridgeLoadDuration.Seconds(),
		},
	}, nil
}

func (h CICDRunCorrelationHandler) emitCounters(ctx context.Context, counts map[CICDRunCorrelationOutcome]int) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range cicdRunCorrelationOutcomes() {
		if counts[outcome] == 0 {
			continue
		}
		h.Instruments.CICDRunCorrelations.Add(ctx, int64(counts[outcome]), metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainCICDRunCorrelation)),
			telemetry.AttrOutcome(string(outcome)),
		))
	}
}

// BuildCICDRunCorrelationDecisions classifies provider runs without turning
// CI success or shell text into deployment truth.
//
// This keeps its existing error-free signature
// ([]facts.Envelope -> []CICDRunCorrelationDecision) so every existing
// table-test caller stays unchanged; it delegates to the quarantine-aware
// buildCICDRunCorrelationDecisionsWithQuarantine and discards the quarantine
// list, matching the pattern
// BuildKubernetesCorrelationDecisions/buildKubernetesCorrelationDecisionsWithQuarantine
// established (go/internal/reducer/AGENTS.md, Wave 4b). CICDRunCorrelationHandler.Handle
// calls the quarantine-aware variant directly so the reducer intent path
// reports quarantines.
func BuildCICDRunCorrelationDecisions(envelopes []facts.Envelope) []CICDRunCorrelationDecision {
	decisions, _, _, err := buildCICDRunCorrelationDecisionsWithQuarantine(envelopes)
	if err != nil {
		// A fatal (non-input_invalid) decode error can only occur for an
		// unsupported schema-version major on the real reducer path, which
		// Handle already surfaces to the caller; every existing test call
		// site here passes schema-version-1 (or unset, normalized to major
		// 1) fixtures, so this branch is unreachable in practice. Returning
		// an empty decision set (rather than panicking) keeps this pure,
		// error-free entry point safe for any caller that has not yet
		// adopted the quarantine-aware signature.
		return nil
	}
	return decisions
}

func cicdRunCorrelationFactKinds() []string {
	return []string{
		facts.CICDRunFactKind,
		facts.CICDArtifactFactKind,
		facts.CICDWorkflowImageEvidenceFactKind,
		facts.CICDEnvironmentObservationFactKind,
		facts.CICDDeploymentEventFactKind,
		facts.CICDTriggerEdgeFactKind,
		facts.CICDStepFactKind,
	}
}

func cicdRunCorrelationOutcomes() []CICDRunCorrelationOutcome {
	return []CICDRunCorrelationOutcome{
		CICDRunCorrelationExact,
		CICDRunCorrelationDerived,
		CICDRunCorrelationAmbiguous,
		CICDRunCorrelationUnresolved,
		CICDRunCorrelationRejected,
	}
}

func cicdRunCorrelationCounts(decisions []CICDRunCorrelationDecision) map[CICDRunCorrelationOutcome]int {
	counts := make(map[CICDRunCorrelationOutcome]int, len(cicdRunCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func cicdRunCorrelationSummary(
	evaluated int,
	preserved int,
	counts map[CICDRunCorrelationOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"ci/cd run correlation evaluated=%d preserved=%d exact=%d derived=%d ambiguous=%d unresolved=%d rejected=%d canonical_writes=%d",
		evaluated,
		preserved,
		counts[CICDRunCorrelationExact],
		counts[CICDRunCorrelationDerived],
		counts[CICDRunCorrelationAmbiguous],
		counts[CICDRunCorrelationUnresolved],
		counts[CICDRunCorrelationRejected],
		canonicalWrites,
	)
}

func cicdRunCorrelationCanonicalWrites(decisions []CICDRunCorrelationDecision) int {
	total := 0
	for _, decision := range decisions {
		total += decision.CanonicalWrites
	}
	return total
}

// emitDeploymentEventSkips reports ci.deployment_event facts the attach dropped
// because the event and its sha-matching run named different repositories.
//
// The drop is total for the affected run and otherwise invisible: the
// collector's deployment_unanchored warning keys on sha rather than repository,
// so it cannot fire for this condition, and validateTarget can only reject a
// PATH disagreement at startup because the run's repository html_url is not
// known until collection. A host disagreement -- an enterprise host, or a typo
// -- therefore reaches here, and without this counter an operator sees every
// deployment event for that run simply not exist.
func (h CICDRunCorrelationHandler) emitDeploymentEventSkips(ctx context.Context, skipped int) {
	if h.Instruments == nil || skipped <= 0 {
		return
	}
	h.Instruments.CICDDeploymentEventsSkipped.Add(ctx, int64(skipped), metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainCICDRunCorrelation)),
		telemetry.AttrSkipReason("repository_mismatch"),
	))
}
