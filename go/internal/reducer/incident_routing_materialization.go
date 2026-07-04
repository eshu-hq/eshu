// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func incidentRoutingMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIncidentRoutingMaterialization,
		Summary: "project exact PagerDuty incident-routing evidence into canonical graph evidence nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "incident_routing_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

const incidentRoutingEvidenceSource = "reducer/incident-routing"

// IncidentRoutingEvidenceLoader loads the RAW incident-routing evidence for one
// scope generation: the incident-context and incident-routing fact envelopes
// (payloads undecoded) plus the declared PagerDuty routing evidence from
// content_entities metadata. The reducer decodes the fact payloads through the
// typed contracts seam itself (buildIncidentRoutingEvidenceInputs), so a
// malformed fact dead-letters as a per-fact input_invalid quarantine rather than
// silently reading an empty-string field in the storage layer.
type IncidentRoutingEvidenceLoader interface {
	LoadIncidentRoutingRawEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) (IncidentRoutingRawEvidence, error)
}

// IncidentRoutingEvidenceWriter writes and retracts reducer-owned
// IncidentRoutingEvidence graph nodes and relationships.
type IncidentRoutingEvidenceWriter interface {
	WriteIncidentRoutingEvidence(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractIncidentRoutingEvidence(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
}

// IncidentRoutingMaterializationHandler reduces one PagerDuty incident-routing
// materialization intent into exact graph evidence rows.
type IncidentRoutingMaterializationHandler struct {
	Loader               IncidentRoutingEvidenceLoader
	Writer               IncidentRoutingEvidenceWriter
	PriorGenerationCheck PriorGenerationCheck
	Instruments          *telemetry.Instruments
}

// Handle executes one incident-routing materialization intent.
func (h IncidentRoutingMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainIncidentRoutingMaterialization {
		return Result{}, fmt.Errorf(
			"incident routing materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Loader == nil {
		return Result{}, fmt.Errorf("incident routing materialization loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("incident routing materialization writer is required")
	}

	loadStart := time.Now()
	raw, err := h.Loader.LoadIncidentRoutingRawEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load incident routing evidence: %w", err)
	}
	loadDuration := time.Since(loadStart)

	// Decode the raw fact payloads through the typed contracts seam. A fact
	// missing a required field is quarantined per-fact as input_invalid and
	// skipped while every valid fact still projects; any other decode error is
	// fatal and aborts the intent for durable triage.
	inputs, quarantined, err := buildIncidentRoutingEvidenceInputs(raw)
	if err != nil {
		return Result{}, fmt.Errorf("decode incident routing evidence: %w", err)
	}

	extractStart := time.Now()
	rows, tally := extractIncidentRoutingRowsForInputs(inputs)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractIncidentRoutingEvidence(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			incidentRoutingEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract incident routing graph evidence: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.Writer.WriteIncidentRoutingEvidence(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			incidentRoutingEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write incident routing graph evidence: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEvidenceCounter(ctx, rows, tally)
	// Emit the visible per-fact dead-letter for every quarantined malformed fact
	// (metric + structured log), and surface the count on the per-intent signal.
	inputInvalidCount := recordQuarantinedFacts(
		ctx, h.Instruments, DomainIncidentRoutingMaterialization, intent.ScopeID, intent.GenerationID, quarantined,
	)
	slog.Info(
		"incident routing materialization completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"incident_count", len(inputs),
		"graph_rows", len(rows),
		"input_invalid_facts", inputInvalidCount,
		"materialized", tally.materialized,
		"skipped", tally.skipped,
		"skip_retract", skipRetract,
		"load_duration_s", loadDuration.Seconds(),
		"extract_duration_s", extractDuration.Seconds(),
		"retract_duration_s", retractDuration.Seconds(),
		"write_duration_s", writeDuration.Seconds(),
		"total_duration_s", time.Since(totalStart).Seconds(),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainIncidentRoutingMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d incident-routing graph evidence row(s) from %d incident packet(s)", len(rows), len(inputs)),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func extractIncidentRoutingRowsForInputs(
	inputs []IncidentRoutingEvidenceInput,
) ([]map[string]any, incidentRoutingProjectionTally) {
	total := newIncidentRoutingProjectionTally()
	var rows []map[string]any
	for _, input := range inputs {
		inputRows, tally := ExtractIncidentRoutingEvidenceRows(input)
		rows = append(rows, inputRows...)
		for key, count := range tally.materialized {
			total.materialized[key] += count
		}
		for key, count := range tally.skipped {
			total.skipped[key] += count
		}
	}
	return rows, total
}

func (h IncidentRoutingMaterializationHandler) recordEvidenceCounter(
	ctx context.Context,
	rows []map[string]any,
	tally incidentRoutingProjectionTally,
) {
	if h.Instruments == nil || h.Instruments.IncidentRoutingEvidence == nil {
		return
	}
	type key struct {
		outcome string
		source  string
		kind    string
	}
	counts := make(map[key]int, len(rows)+len(tally.skipped))
	for _, row := range rows {
		counts[key{
			outcome: incidentRoutingTruthExact,
			source:  firstNonBlank(anyToString(row["source_class"]), "unknown"),
			kind:    firstNonBlank(anyToString(row["slot"]), "routing"),
		}]++
	}
	for outcome, count := range tally.skipped {
		if count == 0 {
			continue
		}
		counts[key{outcome: outcome, source: "provenance", kind: "routing"}] += count
	}
	for key, count := range counts {
		h.Instruments.IncidentRoutingEvidence.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainIncidentRoutingMaterialization)),
			telemetry.AttrOutcome(key.outcome),
			telemetry.AttrSource(key.source),
			telemetry.AttrKind(key.kind),
		))
	}
}

func (h IncidentRoutingMaterializationHandler) shouldSkipRetract(
	ctx context.Context,
	intent Intent,
) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for incident routing retract: %w", err)
	}
	return !hasPrior, nil
}
