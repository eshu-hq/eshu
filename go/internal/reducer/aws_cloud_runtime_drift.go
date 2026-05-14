package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
}

// AWSCloudRuntimeDriftWriteResult summarizes durable AWS runtime drift
// publication.
type AWSCloudRuntimeDriftWriteResult struct {
	CanonicalIDs    []string
	CanonicalWrites int
	EvidenceSummary string
}

// AWSCloudRuntimeDriftHandler evaluates AWS runtime drift evidence and
// publishes admitted orphan/unmanaged findings as durable reducer facts.
type AWSCloudRuntimeDriftHandler struct {
	EvidenceLoader AWSCloudRuntimeDriftEvidenceLoader
	Writer         AWSCloudRuntimeDriftFindingWriter
	Instruments    *telemetry.Instruments
	Logger         *slog.Logger
}

// Handle executes one AWS runtime drift publication intent.
func (h AWSCloudRuntimeDriftHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainAWSCloudRuntimeDrift {
		return Result{}, fmt.Errorf(
			"aws_cloud_runtime_drift handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.EvidenceLoader == nil {
		return Result{}, fmt.Errorf("aws cloud runtime drift evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("aws cloud runtime drift writer is required")
	}

	rows, err := h.EvidenceLoader.LoadAWSCloudRuntimeDriftEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load aws cloud runtime drift evidence: %w", err)
	}

	candidates := cloudruntime.BuildCandidates(rows, intent.ScopeID)
	pack := rules.AWSCloudRuntimeDriftRulePack()
	evaluation, err := engine.Evaluate(pack, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("evaluate aws cloud runtime drift rule pack: %w", err)
	}

	summary := cloudruntime.RecordEvaluation(ctx, h.Instruments, evaluation)
	admitted := admittedAWSCloudRuntimeDriftCandidates(evaluation)
	h.logAdmittedFindings(ctx, intent, admitted)

	writeResult, err := h.Writer.WriteAWSCloudRuntimeDriftFindings(ctx, AWSCloudRuntimeDriftWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Candidates:   admitted,
		Summary:      summary,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write aws cloud runtime drift findings: %w", err)
	}

	return Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
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
		"aws cloud runtime drift evaluated=%d orphaned=%d unmanaged=%d canonical_writes=%d",
		evaluated,
		summary.OrphanedResources,
		summary.UnmanagedResources,
		canonicalWrites,
	)
}

func (h AWSCloudRuntimeDriftHandler) logAdmittedFindings(
	ctx context.Context,
	intent Intent,
	candidates []model.Candidate,
) {
	if h.Logger == nil {
		return
	}
	for _, candidate := range candidates {
		h.Logger.LogAttrs(ctx, slog.LevelInfo, "aws cloud runtime drift finding admitted",
			slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
			slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			slog.String("drift.pack", rules.AWSCloudRuntimeDriftPackName),
			slog.String("drift.kind", awsCloudRuntimeFindingKind(candidate)),
			slog.String("drift.arn", candidate.CorrelationKey),
		)
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
