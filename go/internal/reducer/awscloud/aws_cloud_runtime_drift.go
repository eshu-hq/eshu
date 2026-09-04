// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// AWSCloudRuntimeDriftEvidenceLoader supplies the joined AWS cloud,
// Terraform-state, and Terraform-config rows classified by the
// aws_cloud_runtime_drift rule pack.
type AWSCloudRuntimeDriftEvidenceLoader interface {
	// LoadAWSCloudRuntimeDriftEvidence returns one row per ARN in the AWS
	// runtime scope. Implementations must keep the join bounded to the supplied
	// scope generation and active IaC/state facts for matching ARNs.
	LoadAWSCloudRuntimeDriftEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]cloudruntime.AddressedRow, error)
}

// AWSCloudRuntimeDriftFindingWriter publishes admitted AWS runtime drift
// candidates into the durable canonical truth surface.
type AWSCloudRuntimeDriftFindingWriter interface {
	// WriteAWSCloudRuntimeDriftFindings persists the admitted candidates. The
	// writer must be idempotent by candidate identity so reducer retries do not
	// duplicate findings.
	WriteAWSCloudRuntimeDriftFindings(
		ctx context.Context,
		write AWSCloudRuntimeDriftWrite,
	) (AWSCloudRuntimeDriftWriteResult, error)
}

// AWSCloudRuntimeDriftWrite is the durable publication request for one AWS
// runtime drift reducer intent.
type AWSCloudRuntimeDriftWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Candidates   []model.Candidate
	Summary      cloudruntime.Summary
	// EvidenceAsOf is the moment this write's evidence was read, captured
	// BEFORE the evidence load in Handle (see AWSCloudRuntimeDriftHandler.Handle).
	// Used only for the writer's own required-field guard and as an audit
	// timestamp; it no longer drives the fencing token (#5875 P1 moved that to
	// FencingToken below).
	EvidenceAsOf time.Time
	// FencingToken is the database-issued, cross-worker-ordering value the
	// admission check and fact_records rows are stamped with (#5848, #5875
	// P1). Populated by AWSCloudRuntimeDriftHandler.Handle from
	// AWSCloudRuntimeDriftFencingTokenIssuer, called at the SAME point
	// EvidenceAsOf used to drive the token from: right before the evidence
	// load, so a worker that stalled inside a slow cross-scope load still
	// carries the token value that correctly reflects "evidence read before
	// the stall", not one that could be pushed later by the stall itself. See
	// awsCloudRuntimeDriftFencingToken's doc comment
	// (aws_cloud_runtime_drift_admission.go) for why this must be a
	// database-issued value, not the reducer host's wall clock.
	FencingToken int64
	// EvaluatedARNs is every ARN this pass's evidence load covered, whether or
	// not it produced an admitted candidate. The generation-authoritative
	// retire uses it to bound which prior findings it may remove: an ARN
	// outside this set is left untouched regardless of its fact_id.
	EvaluatedARNs []string
}

// AWSCloudRuntimeDriftWriteResult summarizes durable AWS runtime drift
// publication.
type AWSCloudRuntimeDriftWriteResult struct {
	CanonicalIDs    []string
	CanonicalWrites int
	// Retired counts the stale findings the generation-authoritative retire
	// removed for a reclassified or converged ARN (#5848).
	Retired         int
	EvidenceSummary string
}

// AWSCloudRuntimeDriftHandler evaluates AWS runtime drift evidence and
// publishes admitted orphan/unmanaged findings as durable reducer facts.
type AWSCloudRuntimeDriftHandler struct {
	EvidenceLoader AWSCloudRuntimeDriftEvidenceLoader
	Writer         AWSCloudRuntimeDriftFindingWriter
	Instruments    *telemetry.Instruments
	Logger         *slog.Logger
	// ReadinessChecker reports whether a Terraform state_snapshot scope is still
	// mid-ingestion, so Handle can defer a not-yet-final orphaned_cloud_resource
	// classification instead of writing it as a durable verdict (#5848,
	// #5837's root cause). Nil disables the gate: Handle always writes its
	// best-available classification, matching pre-#5848 behavior.
	ReadinessChecker AWSCloudRuntimeDriftReadinessChecker
	// FencingTokenIssuer supplies the database-issued, cross-worker-ordering
	// fencing token (#5875 P1). Required, like EvidenceLoader and Writer: a
	// nil issuer is a hard Handle() error, never a silent fallback to the
	// host clock -- see AWSCloudRuntimeDriftWrite.FencingToken and
	// awsCloudRuntimeDriftFencingToken's doc comment
	// (aws_cloud_runtime_drift_admission.go) for why a wall-clock fallback
	// would silently reintroduce the exact clock-skew vulnerability this gate
	// exists to close.
	FencingTokenIssuer AWSCloudRuntimeDriftFencingTokenIssuer
	// Now supplies the evidence-read watermark. Left nil it falls back to the
	// process clock; tests inject a deterministic one. See
	// AWSCloudRuntimeDriftWrite.EvidenceAsOf.
	Now func() time.Time
}

// Handle executes one AWS runtime drift publication intent.
func (h AWSCloudRuntimeDriftHandler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainAWSCloudRuntimeDrift {
		return reducercontract.Result{}, fmt.Errorf(
			"aws_cloud_runtime_drift handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.EvidenceLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("aws cloud runtime drift evidence loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("aws cloud runtime drift writer is required")
	}
	if h.FencingTokenIssuer == nil {
		return reducercontract.Result{}, fmt.Errorf("aws cloud runtime drift fencing token issuer is required")
	}

	// Read the fencing watermark BEFORE the evidence load, not after. It must
	// express "how fresh is the world this pass looked at", excluding however
	// long the load, classification, and admission then took (#5848; mirrors
	// containerImageIdentityEvidenceAsOf, #5847).
	evidenceAsOf := factwrite.Now(h.Now)

	// Issue the cross-worker fencing token at the SAME point evidenceAsOf was
	// captured -- before the evidence load, not at write-commit time (#5875
	// P1). This must stay a database-issued value, immune to any individual
	// reducer host's clock; see awsCloudRuntimeDriftFencingToken's doc
	// comment (aws_cloud_runtime_drift_admission.go) for why capturing it
	// here (evidence-read time) rather than in the writer (write-commit time)
	// is load-bearing, not a style choice: a write-time token would order by
	// commit order instead of evidence recency, silently reintroducing the
	// original #5848 bug while fixing this one.
	fencingToken, err := h.FencingTokenIssuer.NextAWSCloudRuntimeDriftFencingToken(ctx)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("issue aws cloud runtime drift fencing token: %w", err)
	}

	// Check readiness BEFORE loading evidence, not after (#5875 P1). See
	// checkAWSCloudRuntimeDriftReadinessBeforeLoad's doc comment for the
	// TOCTOU this ordering closes: a state_snapshot activation landing
	// between a POST-load readiness check and the evidence snapshot it would
	// have checked against could make a stale orphaned_cloud_resource verdict
	// look "ready" to write. The actual defer decision (shouldDeferForLoadedEvidence)
	// runs after classification, combining this pre-load signal with whether
	// the freshly loaded evidence actually contains an orphaned candidate.
	readinessSignal, err := h.checkAWSCloudRuntimeDriftReadinessBeforeLoad(ctx, intent, evidenceAsOf)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("check aws cloud runtime drift state readiness: %w", err)
	}

	rows, err := h.EvidenceLoader.LoadAWSCloudRuntimeDriftEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load aws cloud runtime drift evidence: %w", err)
	}

	candidates := cloudruntime.BuildCandidates(rows, intent.ScopeID)
	pack := rules.AWSCloudRuntimeDriftRulePack()
	evaluation, err := engine.Evaluate(pack, candidates)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("evaluate aws cloud runtime drift rule pack: %w", err)
	}

	admitted := admittedAWSCloudRuntimeDriftCandidates(evaluation)
	summary := summarizeAWSCloudRuntimeDriftCandidates(admitted)

	if shouldDeferForLoadedEvidence(readinessSignal, admitted) {
		h.logStatePendingDefer(ctx, intent, admitted, evidenceAsOf)
		return reducercontract.Result{}, newAWSCloudRuntimeDriftStatePendingError(intent.ScopeID, intent.GenerationID)
	}

	writeResult, err := h.Writer.WriteAWSCloudRuntimeDriftFindings(ctx, AWSCloudRuntimeDriftWrite{
		IntentID:      intent.IntentID,
		ScopeID:       intent.ScopeID,
		GenerationID:  intent.GenerationID,
		SourceSystem:  intent.SourceSystem,
		Cause:         intent.Cause,
		Candidates:    admitted,
		Summary:       summary,
		EvidenceAsOf:  evidenceAsOf,
		FencingToken:  fencingToken,
		EvaluatedARNs: evaluatedAWSCloudRuntimeDriftARNs(rows),
	})
	if err != nil {
		if isAWSCloudRuntimeDriftWriteSuperseded(err) {
			h.logWriteSuperseded(ctx, intent)
		}
		return reducercontract.Result{}, fmt.Errorf("write aws cloud runtime drift findings: %w", err)
	}

	cloudruntime.RecordEvaluation(ctx, h.Instruments, evaluation)
	h.logAdmittedFindings(ctx, intent, admitted)

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: awsCloudRuntimeDriftSummary(
			len(candidates),
			summary,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func admittedAWSCloudRuntimeDriftCandidates(evaluation engine.Evaluation) []model.Candidate {
	out := make([]model.Candidate, 0, len(evaluation.Results))
	for _, result := range evaluation.Results {
		if result.Candidate.State == model.CandidateStateAdmitted {
			out = append(out, result.Candidate)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CorrelationKey != out[j].CorrelationKey {
			return out[i].CorrelationKey < out[j].CorrelationKey
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func awsCloudRuntimeDriftSummary(
	evaluated int,
	summary cloudruntime.Summary,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"aws cloud runtime drift evaluated=%d orphaned=%d unmanaged=%d ambiguous=%d unknown=%d "+
			"image_version_drift=%d value_comparison_inconclusive=%d canonical_writes=%d",
		evaluated,
		summary.OrphanedResources,
		summary.UnmanagedResources,
		summary.AmbiguousResources,
		summary.UnknownResources,
		summary.ImageVersionDriftResources,
		summary.ValueComparisonInconclusiveResources,
		canonicalWrites,
	)
}

func summarizeAWSCloudRuntimeDriftCandidates(candidates []model.Candidate) cloudruntime.Summary {
	var summary cloudruntime.Summary
	for _, candidate := range candidates {
		switch cloudruntime.FindingKind(awsCloudRuntimeFindingKind(candidate)) {
		case cloudruntime.FindingKindOrphanedCloudResource:
			summary.OrphanedResources++
		case cloudruntime.FindingKindUnmanagedCloudResource:
			summary.UnmanagedResources++
		case cloudruntime.FindingKindAmbiguousCloudResource:
			summary.AmbiguousResources++
		case cloudruntime.FindingKindUnknownCloudResource:
			summary.UnknownResources++
		case cloudruntime.FindingKindImageVersionDrift:
			summary.ImageVersionDriftResources++
		case cloudruntime.FindingKindValueComparisonInconclusive:
			summary.ValueComparisonInconclusiveResources++
		}
	}
	return summary
}

func (h AWSCloudRuntimeDriftHandler) logAdmittedFindings(
	ctx context.Context,
	intent reducercontract.Intent,
	candidates []model.Candidate,
) {
	if h.Logger == nil {
		return
	}
	for _, candidate := range candidates {
		attrs := []slog.Attr{
			log.Domain(string(intent.Domain)),
			log.ScopeID(intent.ScopeID),
			log.GenerationID(intent.GenerationID),
			slog.String("drift.pack", rules.AWSCloudRuntimeDriftPackName),
			slog.String("drift.kind", awsCloudRuntimeFindingKind(candidate)),
		}
		attrs = append(attrs, telemetry.SafeResourceLogAttrs(candidate.CorrelationKey)...)
		h.Logger.LogAttrs(ctx, slog.LevelInfo, "aws cloud runtime drift finding admitted", attrs...)
	}
}

func awsCloudRuntimeFindingKind(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == cloudruntime.EvidenceTypeFindingKind {
			return strings.TrimSpace(atom.Value)
		}
	}
	return ""
}
