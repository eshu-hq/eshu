// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
// scope generation. It is satisfied both by the fixpoint evidence loader
// (ValueFlowFixpointEvidenceLoader, which SOLVES the cross-repo value-flow
// program from persisted summaries and therefore has no raw fact to decode)
// and, historically, by the postgres raw-fact loader. The materialization
// handler no longer uses this interface for raw facts — it uses
// CodeInterprocEvidenceFactLoader so it can decode + quarantine — but the
// fixpoint projector (ValueFlowFixpointEvidenceProjector) still consumes this
// typed-input interface because its inputs come from an in-memory solve, not
// a raw decode.
type CodeInterprocEvidenceLoader interface {
	LoadCodeInterprocEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CodeInterprocEvidenceInput, error)
}

// CodeInterprocEvidenceFactLoader loads the raw code_interproc_evidence fact
// envelopes for one scope generation. The materialization handler decodes them
// through the typed contracts seam (ExtractCodeInterprocEvidenceRowsWithQuarantine)
// so a malformed fact dead-letters as an input_invalid quarantine rather than
// being silently dropped by the loader (Contract System v1 Wave 4f S2, issue
// #4754). This is separate from CodeInterprocEvidenceLoader because the
// fixpoint projector's loader produces already-typed inputs from an in-memory
// solve and has no envelopes to hand back.
type CodeInterprocEvidenceFactLoader interface {
	LoadCodeInterprocEvidenceFacts(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]facts.Envelope, error)
}

// CodeInterprocEvidenceWriter writes and retracts reducer-owned TAINT_FLOWS_TO
// edges between Function nodes.
type CodeInterprocEvidenceWriter interface {
	WriteCodeInterprocEvidence(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCodeInterprocEvidence(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
	RetractCodeInterprocEvidenceSource(ctx context.Context, evidenceSource string) error
	RetractCodeInterprocEvidenceByUIDs(ctx context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string) error
	RetractCodeInterprocEvidenceSourceByUIDs(ctx context.Context, sourceUIDs []string, evidenceSource string) error
	RetractStaleCodeInterprocEvidenceByUIDs(ctx context.Context, sourceUIDs []string, scopeID, generationID, evidenceSource string) error
}

// sourceUIDsFromRows extracts distinct source_function_uid values from edge rows.
func sourceUIDsFromRows(rows []map[string]any) []string {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if uid, ok := row["source_function_uid"].(string); ok && uid != "" {
			seen[uid] = struct{}{}
		}
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	return uids
}

// CodeInterprocEvidenceMaterializationHandler reduces one cross-function
// evidence intent into TAINT_FLOWS_TO edge rows.
type CodeInterprocEvidenceMaterializationHandler struct {
	Loader               CodeInterprocEvidenceFactLoader
	Writer               CodeInterprocEvidenceWriter
	Ledger               CodeInterprocProjectedEdgeLedger
	PriorGenerationCheck PriorGenerationCheck
	Instruments          *telemetry.Instruments
}

// Handle executes one cross-function evidence materialization intent: load the
// resolved findings, project them to edge rows, retract the prior generation's
// edges (unless this is the first generation for the scope), and write the rows.
// When a Ledger is present, retraction enumerates source Function uids from the
// ledger and uses anchored-delete; the ledger is recorded before the graph edge
// write so it is always a superset of graph edges.
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

	envelopes, err := h.Loader.LoadCodeInterprocEvidenceFacts(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load code interproc evidence: %w", err)
	}
	rows, quarantined, err := ExtractCodeInterprocEvidenceRowsWithQuarantine(envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("decode code interproc evidence: %w", err)
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainCodeInterprocEvidence, intent.ScopeID, intent.GenerationID, quarantined)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if h.Ledger != nil {
			uids, err := h.Ledger.ListSourceUIDsForScopes(ctx, codeInterprocEvidenceSource, []string{intent.ScopeID})
			if err != nil {
				return Result{}, fmt.Errorf("list source uids for retract: %w", err)
			}
			if err := h.Writer.RetractCodeInterprocEvidenceByUIDs(
				ctx, uids, []string{intent.ScopeID}, codeInterprocEvidenceSource,
			); err != nil {
				return Result{}, fmt.Errorf("retract code interproc evidence by uids: %w", err)
			}
			if err := h.Ledger.PruneForScopes(ctx, codeInterprocEvidenceSource, []string{intent.ScopeID}); err != nil {
				return Result{}, fmt.Errorf("prune code interproc projected edges: %w", err)
			}
		} else {
			if err := h.Writer.RetractCodeInterprocEvidence(
				ctx, []string{intent.ScopeID}, intent.GenerationID, codeInterprocEvidenceSource,
			); err != nil {
				return Result{}, fmt.Errorf("retract code interproc evidence: %w", err)
			}
		}
	}
	if len(rows) > 0 {
		if h.Ledger != nil {
			uids := sourceUIDsFromRows(rows)
			if len(uids) > 0 {
				if err := h.Ledger.RecordProjectedEdges(
					ctx, codeInterprocEvidenceSource, intent.ScopeID, intent.GenerationID,
					uids, time.Now(),
				); err != nil {
					return Result{}, fmt.Errorf("record projected edges: %w", err)
				}
			}
		}
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
		"fact_count", len(envelopes),
		"graph_rows", len(rows),
		"input_invalid_facts", inputInvalidCount,
		"skip_retract", skipRetract,
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeInterprocEvidence,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d cross-function taint edge(s) from %d fact(s)",
			len(rows),
			len(envelopes),
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
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
