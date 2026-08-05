// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/facts"
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
	FactLoader  FactLoader
	Writer      CICDRunCorrelationWriter
	Instruments *telemetry.Instruments
	// ProvenanceEdgeWriter projects exact workflow-image correlations into
	// independently owned BUILT_FROM graph assertions when configured.
	ProvenanceEdgeWriter ContainerImageProvenanceEdgeWriter
}

// Handle executes one CI/CD run correlation reducer intent.
func (h CICDRunCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCICDRunCorrelation {
		return Result{}, fmt.Errorf("ci_cd_run_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ci/cd run correlation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("ci/cd run correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, cicdRunCorrelationFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load ci/cd run correlation facts: %w", err)
	}
	envelopes, patchGeneration, err := h.loadCICDRunCorrelationPatchFacts(ctx, intent, envelopes)
	if err != nil {
		return Result{}, err
	}
	workflowBridgeLoadStarted := time.Now()
	workflowImages, err := h.loadActiveCICDWorkflowImageFacts(ctx, cicdRunRepositoryIDs(envelopes))
	workflowBridgeLoadDuration := time.Since(workflowBridgeLoadStarted)
	if err != nil {
		return Result{}, fmt.Errorf("load active git workflow image facts: %w", err)
	}
	envelopes = append(envelopes, workflowImages...)
	active, err := h.loadActiveCICDRunCorrelationFacts(ctx, ciArtifactDigests(envelopes), ciWorkflowImageRefs(envelopes))
	if err != nil {
		return Result{}, fmt.Errorf("load active ci/cd artifact identity facts: %w", err)
	}
	envelopes = append(envelopes, active...)

	decisions, quarantined, deploymentEventsSkipped, err := buildCICDRunCorrelationDecisionsWithQuarantine(envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("build ci/cd run correlation decisions: %w", err)
	}
	evaluatedCount := len(decisions)
	preservedCount := 0
	if patchGeneration && len(decisions) > maxCICDRunCorrelationPatchDecisions {
		return Result{}, fmt.Errorf(
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
		return Result{}, fmt.Errorf("write ci/cd run correlations: %w", err)
	}
	if err := h.projectCICDWorkflowImageBuiltFromEdges(ctx, intent, decisions); err != nil {
		return Result{}, err
	}
	h.emitCounters(ctx, counts)
	h.emitDeploymentEventSkips(ctx, deploymentEventsSkipped)
	quarantinedCount := recordQuarantinedFacts(ctx, h.Instruments, DomainCICDRunCorrelation, intent.ScopeID, intent.GenerationID, quarantined)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCICDRunCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: cicdRunCorrelationSummary(evaluatedCount, preservedCount, counts, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
		SubSignals:      inputInvalidSubSignals(quarantinedCount),
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
			telemetry.AttrDomain(string(DomainCICDRunCorrelation)),
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

func classifyCICDRunEvidence(ev *cicdRunEvidence, imageIndex map[string][]cicdImageIdentity) CICDRunCorrelationDecision {
	run := ev.runDecoded
	// Every identity/anchor field is trimmed to preserve byte-parity with the
	// pre-migration payloadString read path (see trimmedCICDField/
	// trimmedCICDPtr in ci_cd_run_correlation_decode.go). RepositoryID and
	// CommitSHA feed the unresolved-anchor emptiness check below, so a
	// whitespace-only value must count as empty exactly as the old trimmed
	// path did; Provider/RunID/RunAttempt are the persisted decision identity
	// and must match the trimmed join key.
	decision := CICDRunCorrelationDecision{
		Provider:         trimmedCICDField(run.Provider),
		RunID:            trimmedCICDField(run.RunID),
		RunAttempt:       defaultCICDRunAttempt(derefString(run.RunAttempt)),
		RepositoryID:     trimmedCICDPtr(run.RepositoryID),
		CommitSHA:        trimmedCICDPtr(run.CommitSHA),
		Outcome:          CICDRunCorrelationDerived,
		Reason:           "run has provider evidence but no explicit artifact identity anchor",
		ProvenanceOnly:   true,
		CorrelationKind:  "run_evidence",
		SourceLayerKinds: []string{"reported"},
		EvidenceFactIDs:  []string{ev.run.FactID},
	}
	// An attached deployment event (repo/commit-scoped evidence, see
	// attachDeploymentEventsToRuns) beats the declared job/step-level
	// environment observation: the GitHub Deployments API is the platform's
	// own record of what environment a commit was actually deployed to,
	// where an environment_observation is inferred from the run's own
	// job/step configuration and can drift from what really happened.
	if env, factID, ok := classifyCICDDeploymentEventEnvironment(ev.deploymentEvents); ok {
		decision.Environment = env
		decision.EnvironmentEvidence = supplyChainEnvironmentEvidenceDeployEvent
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, factID)
	} else if len(ev.environmentsDecoded) > 0 {
		decision.Environment = environment.Canonical(trimmedCICDPtr(ev.environmentsDecoded[0].Environment))
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.environments[0].FactID)
		decision.EnvironmentEvidence = supplyChainEnvironmentEvidenceDeclared
	}
	for _, trigger := range ev.triggers {
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, trigger.FactID)
	}
	if decision.RepositoryID == "" || decision.CommitSHA == "" {
		decision.Outcome = CICDRunCorrelationUnresolved
		decision.Reason = "run is missing repository_id or commit_sha anchor"
		return decision
	}
	if len(ev.shellOnly) > 0 {
		decision.Outcome = CICDRunCorrelationRejected
		decision.Reason = "shell-only deployment hint suppressed without artifact identity"
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.shellOnly[0].FactID)
		return decision
	}
	if workflowDecision, ok := classifyCICDWorkflowImageEvidence(decision, ev.workflowImages, ev.workflowImagesCommitMatched, imageIndex); ok {
		return workflowDecision
	}
	for i, artifact := range ev.artifactsDecoded {
		digest := trimmedCICDPtr(artifact.ArtifactDigest)
		if digest == "" {
			continue
		}
		decision.ArtifactDigest = digest
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.artifacts[i].FactID)
		matches := imageIndex[digest]
		if repoMatches := cicdImageMatchesForRepository(matches, decision.RepositoryID); len(repoMatches) > 0 {
			matches = repoMatches
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			decision.Outcome = CICDRunCorrelationExact
			decision.Reason = "artifact digest matches one container image identity row"
			decision.ProvenanceOnly = false
			decision.CanonicalWrites = 1
			decision.CanonicalTarget = "container_image"
			decision.CorrelationKind = "artifact_image"
			decision.ImageRef = matches[0].imageRef
			decision.EvidenceFactIDs = append(
				decision.EvidenceFactIDs,
				cicdImageIdentityEvidenceFactIDs(matches[0])...,
			)
			decision.SourceLayerKinds = []string{"reported", "observed_resource"}
			return decision
		default:
			decision.Outcome = CICDRunCorrelationAmbiguous
			decision.Reason = "artifact digest matches multiple container image identity rows"
			decision.CorrelationKind = "artifact_image"
			for _, match := range matches {
				decision.EvidenceFactIDs = append(
					decision.EvidenceFactIDs,
					cicdImageIdentityEvidenceFactIDs(match)...,
				)
			}
			return decision
		}
	}
	return decision
}

// cicdImageMatchesForRepository narrows a digest's container_image_identity
// matches to those the run's own repository built, so a single surviving row
// can promote the correlation to exact.
//
// It joins on build_provenance_repository_ids, and on nothing else:
//
//   - The identity's own repository_id is the OCI registry's identifier
//     ("oci-registry://ghcr.io/org/repo"), a namespace disjoint from the
//     canonical "repository:r_..." ids a ci.run carries. Narrowing compared
//     those directly, so it never matched and left the unfiltered multi-row set
//     to degrade an otherwise-exact correlation into ambiguous (#5766, the same
//     trap #5464 found on the supply-chain side). That field is no longer
//     decoded at all.
//   - source_repository_ids conflates "this repository built the image" with
//     "this repository's manifest merely references the digest". Narrowing on it
//     can select a reference-only row and promote a repository that only deploys
//     the image to exact -- the conflation #5796 fixed inside the identity
//     domain by gating its own BUILT_FROM projection on the narrower set
//     (#5823).
//
// A row published before #5823 carries no build-provenance key, so it can never
// be selected here. That is deliberate, and it is NOT a degradation: because the
// pre-#5766 predicate compared the OCI repository_id, narrowing was already a
// dead no-op for every such row, and a legacy multi-row digest already resolved
// ambiguous. Falling back to source_repository_ids for those rows would not
// recover a lost correlation -- there is none to recover -- it would only
// manufacture a false exact that the previous behavior never produced. The
// sharper join engages for a scope as soon as its identity intent republishes.
func cicdImageMatchesForRepository(matches []cicdImageIdentity, repositoryID string) []cicdImageIdentity {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return nil
	}
	out := make([]cicdImageIdentity, 0, len(matches))
	for _, match := range matches {
		for _, candidate := range match.buildProvenanceRepositoryIDs {
			if candidate == repositoryID {
				out = append(out, match)
				break
			}
		}
	}
	return out
}

func defaultCICDRunAttempt(attempt string) string {
	if strings.TrimSpace(attempt) == "" {
		return "1"
	}
	return strings.TrimSpace(attempt)
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
		telemetry.AttrDomain(string(DomainCICDRunCorrelation)),
		telemetry.AttrSkipReason("repository_mismatch"),
	))
}
