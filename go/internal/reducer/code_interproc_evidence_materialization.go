// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// codeInterprocEvidenceSource is the evidence-source tag for reducer-owned
// TAINT_FLOWS_TO edges, used for scoped retraction before reprojection.
const (
	codeInterprocEvidenceSource         = "reducer/code-interproc"
	codeInterprocFixpointEvidenceSource = "reducer/code-interproc-fixpoint"
)

func codeInterprocEvidenceDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCodeInterprocEvidence,
		Summary: "project cross-function value-flow findings into TAINT_FLOWS_TO edges between Function nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_interproc_evidence",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// CodeInterprocEvidenceLoader loads reducer-ready cross-function findings for one
// scope generation.
type CodeInterprocEvidenceLoader interface {
	LoadCodeInterprocEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CodeInterprocEvidenceInput, error)
}

// CodeInterprocEvidenceWriter writes and retracts reducer-owned TAINT_FLOWS_TO
// edges between Function nodes.
type CodeInterprocEvidenceWriter interface {
	WriteCodeInterprocEvidence(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCodeInterprocEvidence(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
	RetractCodeInterprocEvidenceSource(ctx context.Context, evidenceSource string) error
}

// CodeInterprocEvidenceMaterializationHandler reduces one cross-function
// evidence intent into TAINT_FLOWS_TO edge rows.
type CodeInterprocEvidenceMaterializationHandler struct {
	Loader               CodeInterprocEvidenceLoader
	Writer               CodeInterprocEvidenceWriter
	PriorGenerationCheck PriorGenerationCheck
	Instruments          *telemetry.Instruments
}

// Handle executes one cross-function evidence materialization intent: load the
// resolved findings, project them to edge rows, retract the prior generation's
// edges (unless this is the first generation for the scope), and write the rows.
func (h CodeInterprocEvidenceMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeInterprocEvidence {
		return Result{}, fmt.Errorf("code interproc evidence handler does not accept domain %q", intent.Domain)
	}
	if h.Loader == nil {
		return Result{}, fmt.Errorf("code interproc evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("code interproc evidence writer is required")
	}

	inputs, err := h.Loader.LoadCodeInterprocEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load code interproc evidence: %w", err)
	}
	rows := ExtractCodeInterprocEvidenceRows(inputs)
	unresolvedEndpointCount := unresolvedCodeInterprocEndpointCount(inputs)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if err := h.Writer.RetractCodeInterprocEvidence(
			ctx, []string{intent.ScopeID}, intent.GenerationID, codeInterprocEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract code interproc evidence: %w", err)
		}
	}
	if len(rows) > 0 {
		if err := h.Writer.WriteCodeInterprocEvidence(
			ctx, rows, intent.ScopeID, intent.GenerationID, codeInterprocEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write code interproc evidence: %w", err)
		}
	}

	slog.Info(
		"code interproc evidence materialization completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"finding_count", len(inputs),
		"graph_rows", len(rows),
		"unresolved_endpoint_count", unresolvedEndpointCount,
		"skip_retract", skipRetract,
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeInterprocEvidence,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d cross-function taint edge(s) from %d finding(s), skipped %d unresolved endpoint finding(s)",
			len(rows),
			len(inputs),
			unresolvedEndpointCount,
		),
		CanonicalWrites: len(rows),
	}, nil
}

// shouldSkipRetract reports whether the pre-write retraction must be skipped: on
// the first attempt of the first generation for a scope there is nothing to
// retract.
func (h CodeInterprocEvidenceMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for code interproc evidence retract: %w", err)
	}
	return !hasPrior, nil
}

func unresolvedCodeInterprocEndpointCount(inputs []CodeInterprocEvidenceInput) int {
	count := 0
	for _, input := range inputs {
		if input.SourceFunctionUID == "" || input.SinkFunctionUID == "" {
			count++
		}
	}
	return count
}
