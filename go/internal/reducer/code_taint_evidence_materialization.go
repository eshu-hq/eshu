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

// codeTaintEvidenceSource is the evidence-source tag for reducer-owned
// CodeTaintEvidence graph writes, used for scoped retraction before reprojection.
const codeTaintEvidenceSource = "reducer/code-taint"

func codeTaintEvidenceDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCodeTaintEvidence,
		Summary: "project value-flow taint findings into graph evidence nodes attached to their Function",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_taint_evidence",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// CodeTaintEvidenceLoader loads reducer-ready taint findings for one scope
// generation.
type CodeTaintEvidenceLoader interface {
	LoadCodeTaintEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CodeTaintEvidenceInput, error)
}

// CodeTaintEvidenceWriter writes and retracts reducer-owned CodeTaintEvidence
// graph nodes and Function relationships.
type CodeTaintEvidenceWriter interface {
	WriteCodeTaintEvidence(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCodeTaintEvidence(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
}

// CodeTaintEvidenceMaterializationHandler reduces one taint-evidence intent into
// graph evidence rows.
type CodeTaintEvidenceMaterializationHandler struct {
	Loader               CodeTaintEvidenceLoader
	Writer               CodeTaintEvidenceWriter
	PriorGenerationCheck PriorGenerationCheck
	Instruments          *telemetry.Instruments
}

// Handle executes one taint-evidence materialization intent: load the resolved
// findings, project them to rows, retract the prior generation's nodes (unless
// this is the first generation for the scope), and write the new rows.
func (h CodeTaintEvidenceMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeTaintEvidence {
		return Result{}, fmt.Errorf("code taint evidence handler does not accept domain %q", intent.Domain)
	}
	if h.Loader == nil {
		return Result{}, fmt.Errorf("code taint evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("code taint evidence writer is required")
	}

	inputs, err := h.Loader.LoadCodeTaintEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load code taint evidence: %w", err)
	}
	rows := ExtractCodeTaintEvidenceRows(inputs)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if err := h.Writer.RetractCodeTaintEvidence(
			ctx, []string{intent.ScopeID}, intent.GenerationID, codeTaintEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract code taint evidence: %w", err)
		}
	}
	if len(rows) > 0 {
		if err := h.Writer.WriteCodeTaintEvidence(
			ctx, rows, intent.ScopeID, intent.GenerationID, codeTaintEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write code taint evidence: %w", err)
		}
	}

	slog.Info(
		"code taint evidence materialization completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"finding_count", len(inputs),
		"graph_rows", len(rows),
		"skip_retract", skipRetract,
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCodeTaintEvidence,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d taint evidence row(s) from %d finding(s)", len(rows), len(inputs)),
		CanonicalWrites: len(rows),
	}, nil
}

// shouldSkipRetract reports whether the pre-write retraction must be skipped:
// on the first attempt of the first generation for a scope there is nothing to
// retract, so the sweep is avoided.
func (h CodeTaintEvidenceMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for code taint evidence retract: %w", err)
	}
	return !hasPrior, nil
}
