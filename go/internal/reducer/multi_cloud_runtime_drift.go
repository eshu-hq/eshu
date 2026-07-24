// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// MultiCloudRuntimeDriftEvidenceLoader supplies the joined provider-neutral
// cloud, Terraform-state, and Terraform-config rows classified by the
// multi_cloud_runtime_drift rule pack. Rows are keyed on canonical
// cloud_resource_uid so AWS, GCP, and Azure share one join.
type MultiCloudRuntimeDriftEvidenceLoader interface {
	// LoadMultiCloudRuntimeDriftEvidence returns one row per canonical identity
	// in the runtime scope. Implementations must keep the join bounded to the
	// supplied scope generation and active IaC/state facts for matching
	// identities, and must not fabricate canonical uids for unresolved rows.
	LoadMultiCloudRuntimeDriftEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]multicloud.Row, error)
}

// MultiCloudRuntimeDriftFindingWriter publishes admitted provider-neutral runtime
// drift candidates into the durable canonical truth surface.
type MultiCloudRuntimeDriftFindingWriter interface {
	// WriteMultiCloudRuntimeDriftFindings persists the admitted candidates. The
	// writer must be idempotent by candidate identity so reducer retries and
	// replays do not duplicate findings.
	WriteMultiCloudRuntimeDriftFindings(
		ctx context.Context,
		write MultiCloudRuntimeDriftWrite,
	) (MultiCloudRuntimeDriftWriteResult, error)
}

// MultiCloudRuntimeDriftWrite is the durable publication request for one
// multi-cloud runtime drift reducer intent.
type MultiCloudRuntimeDriftWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Candidates   []model.Candidate
	Summary      multicloud.Summary
}

// MultiCloudRuntimeDriftWriteResult summarizes durable multi-cloud runtime drift
// publication.
type MultiCloudRuntimeDriftWriteResult struct {
	CanonicalIDs    []string
	CanonicalWrites int
	EvidenceSummary string
}

// MultiCloudRuntimeDriftHandler evaluates provider-neutral runtime drift evidence
// and publishes admitted orphan/unmanaged/ambiguous/unknown findings as durable
// reducer facts. It mirrors AWSCloudRuntimeDriftHandler but joins on canonical
// cloud_resource_uid so providers share one drift path.
type MultiCloudRuntimeDriftHandler struct {
	EvidenceLoader MultiCloudRuntimeDriftEvidenceLoader
	Writer         MultiCloudRuntimeDriftFindingWriter
	Instruments    *telemetry.Instruments
	Logger         *slog.Logger
}

// Handle executes one multi-cloud runtime drift publication intent.
func (h MultiCloudRuntimeDriftHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainMultiCloudRuntimeDrift {
		return Result{}, fmt.Errorf(
			"multi_cloud_runtime_drift handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.EvidenceLoader == nil {
		return Result{}, fmt.Errorf("multi cloud runtime drift evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("multi cloud runtime drift writer is required")
	}

	rows, err := h.EvidenceLoader.LoadMultiCloudRuntimeDriftEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load multi cloud runtime drift evidence: %w", err)
	}

	candidates := multicloud.BuildCandidates(rows, intent.ScopeID)
	pack := rules.MultiCloudRuntimeDriftRulePack()
	evaluation, err := engine.Evaluate(pack, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("evaluate multi cloud runtime drift rule pack: %w", err)
	}

	admitted := admittedMultiCloudRuntimeDriftCandidates(evaluation)
	summary := summarizeMultiCloudRuntimeDriftCandidates(admitted)

	writeResult, err := h.Writer.WriteMultiCloudRuntimeDriftFindings(ctx, MultiCloudRuntimeDriftWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Candidates:   admitted,
		Summary:      summary,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write multi cloud runtime drift findings: %w", err)
	}

	multicloud.RecordEvaluation(ctx, h.Instruments, evaluation)
	h.logAdmittedFindings(ctx, intent, admitted)

	return Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: multiCloudRuntimeDriftSummary(
			len(candidates),
			summary,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func admittedMultiCloudRuntimeDriftCandidates(evaluation engine.Evaluation) []model.Candidate {
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

func multiCloudRuntimeDriftSummary(
	evaluated int,
	summary multicloud.Summary,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"multi cloud runtime drift evaluated=%d orphaned=%d unmanaged=%d ambiguous=%d unknown=%d image_version_drift=%d canonical_writes=%d",
		evaluated,
		summary.OrphanedResources,
		summary.UnmanagedResources,
		summary.AmbiguousResources,
		summary.UnknownResources,
		summary.ImageVersionDriftResources,
		canonicalWrites,
	)
}

func summarizeMultiCloudRuntimeDriftCandidates(candidates []model.Candidate) multicloud.Summary {
	var summary multicloud.Summary
	for _, candidate := range candidates {
		switch cloudruntime.FindingKind(multicloud.FindingKindFromCandidate(candidate)) {
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
		}
	}
	return summary
}

func (h MultiCloudRuntimeDriftHandler) logAdmittedFindings(
	ctx context.Context,
	intent Intent,
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
			slog.String("drift.pack", rules.MultiCloudRuntimeDriftPackName),
			slog.String("drift.kind", multicloud.FindingKindFromCandidate(candidate)),
			slog.String("drift.provider", multicloud.ProviderFromCandidate(candidate)),
		}
		attrs = append(attrs, telemetry.SafeResourceLogAttrs(candidate.CorrelationKey)...)
		h.Logger.LogAttrs(ctx, slog.LevelInfo, "multi cloud runtime drift finding admitted", attrs...)
	}
}

func multiCloudRuntimeFindingKind(candidate model.Candidate) string {
	return strings.TrimSpace(multicloud.FindingKindFromCandidate(candidate))
}
