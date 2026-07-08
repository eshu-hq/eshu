// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

func azureRelationshipMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAzureRelationshipMaterialization,
		Summary: "project azure_cloud_relationship facts into canonical Azure relationship graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "azure_relationship_materialization",
			SourceLayers:  []truth.Layer{truth.LayerObservedResource},
		},
	}
}

const (
	azureRelationshipEvidenceSource = "reducer/azure-relationships"
)

// AzureRelationshipMaterializationHandler reduces one Azure relationship
// materialization intent into canonical CloudResource relationship edges. It
// gates on Azure CloudResource node readiness and resolves endpoints only
// through exact normalized ARM resource ids from materialized resource facts.
type AzureRelationshipMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           CloudResourceEdgeWriter
	ReadinessLookup      GraphProjectionReadinessLookup
	PriorGenerationCheck PriorGenerationCheck
	// Ledger records and enumerates source CloudResource uids of projected
	// Azure relationship edges so retraction can enumerate uids from the
	// ledger and use anchored-delete instead of scanning the whole
	// :CloudResource label. Nil preserves the pre-ledger whole-scope retract
	// (RetractCloudResourceEdges).
	Ledger ProjectedSourceLedger
	Tracer trace.Tracer
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// for a per-fact quarantined azure_cloud_resource/azure_cloud_relationship
	// decode failure. A nil Instruments only skips the counter increment; the
	// quarantine and structured error log still happen via
	// recordQuarantinedFacts.
	Instruments *telemetry.Instruments
}

// Handle executes one Azure relationship materialization intent.
func (h AzureRelationshipMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainAzureRelationshipMaterialization {
		return Result{}, fmt.Errorf("azure relationship materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("azure relationship materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("azure relationship materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerAzureRelationshipMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return Result{}, azureRelationshipNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, []string{
		facts.AzureCloudResourceFactKind,
		facts.AzureCloudRelationshipFactKind,
	})
	if err != nil {
		return Result{}, fmt.Errorf("load facts for azure relationship materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, relationshipEnvelopes := splitAzureFactEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractAzureRelationshipEdgeRows(resourceEnvelopes, relationshipEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed azure_cloud_resource or
	// azure_cloud_relationship fact is quarantined as a visible input_invalid
	// dead-letter — counter + structured error log — while every valid fact
	// still materializes its edge below.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainAzureRelationshipMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if h.Ledger != nil {
			uids, err := h.Ledger.ListSourceUIDsForScopes(ctx, azureRelationshipEvidenceSource, []string{intent.ScopeID})
			if err != nil {
				return Result{}, fmt.Errorf("list source uids for azure relationship retract: %w", err)
			}
			if err := h.EdgeWriter.RetractCloudResourceEdgesByUIDs(
				ctx, uids, []string{intent.ScopeID}, azureRelationshipEvidenceSource,
			); err != nil {
				return Result{}, fmt.Errorf("retract canonical azure relationship edges by uids: %w", err)
			}
			if err := h.Ledger.PruneForScopes(ctx, azureRelationshipEvidenceSource, []string{intent.ScopeID}); err != nil {
				return Result{}, fmt.Errorf("prune azure relationship projected sources: %w", err)
			}
		} else if err := h.EdgeWriter.RetractCloudResourceEdges(ctx, []string{intent.ScopeID}, intent.GenerationID, azureRelationshipEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("retract canonical azure relationship edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		if h.Ledger != nil {
			uids := sourceUIDsFromRowsByKey(rows, "source_uid")
			if len(uids) > 0 {
				if err := h.Ledger.RecordProjectedSources(
					ctx, azureRelationshipEvidenceSource, intent.ScopeID, intent.GenerationID, uids, time.Now(),
				); err != nil {
					return Result{}, fmt.Errorf("record azure relationship projected sources: %w", err)
				}
			}
		}
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCloudResourceEdges(ctx, rows, intent.ScopeID, intent.GenerationID, azureRelationshipEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical azure relationship edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	timing := azureRelationshipMaterializationTiming{
		intent:            intent,
		resourceFactCount: len(resourceEnvelopes),
		relationshipCount: len(relationshipEnvelopes),
		edgeCount:         len(rows),
		tally:             tally,
		skipRetract:       skipRetract,
		loadDuration:      loadDuration,
		extractDuration:   extractDuration,
		retractDuration:   retractDuration,
		writeDuration:     writeDuration,
		totalDuration:     time.Since(totalStart),
	}
	logAzureRelationshipMaterializationCompleted(ctx, timing)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainAzureRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d azure relationship edge(s) from %d relationship fact(s); %d skipped",
			len(rows),
			len(relationshipEnvelopes),
			tally.skippedCount(),
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h AzureRelationshipMaterializationHandler) canonicalNodesReady(intent Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := graphProjectionPhaseStateForIntent(intent, GraphProjectionKeyspaceCloudResourceUID, GraphProjectionPhaseCanonicalNodesCommitted, time.Now().UTC())
	if !ok {
		return false
	}
	ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
	return found && ready
}

func (h AzureRelationshipMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for azure relationship retract: %w", err)
	}
	return !hasPrior, nil
}

func splitAzureFactEnvelopes(envelopes []facts.Envelope) (resources, relationships []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AzureCloudResourceFactKind:
			resources = append(resources, env)
		case facts.AzureCloudRelationshipFactKind:
			relationships = append(relationships, env)
		}
	}
	return resources, relationships
}

type azureRelationshipNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e azureRelationshipNodesNotReadyError) Error() string {
	return fmt.Sprintf("canonical azure cloud resource nodes not committed for scope %s generation %s", e.scopeID, e.generationID)
}

func (azureRelationshipNodesNotReadyError) Retryable() bool { return true }

func (azureRelationshipNodesNotReadyError) FailureClass() string {
	return "azure_relationship_nodes_not_ready"
}

type azureRelationshipMaterializationTiming struct {
	intent            Intent
	resourceFactCount int
	relationshipCount int
	edgeCount         int
	tally             azureRelationshipEdgeTally
	skipRetract       bool
	loadDuration      time.Duration
	extractDuration   time.Duration
	retractDuration   time.Duration
	writeDuration     time.Duration
	totalDuration     time.Duration
}

func logAzureRelationshipMaterializationCompleted(ctx context.Context, timing azureRelationshipMaterializationTiming) {
	slog.InfoContext(
		ctx, "azure relationship materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("azure_resource_fact_count", timing.resourceFactCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("resolved_count", timing.tally.resolvedCount()),
		slog.Int("skipped_count", timing.tally.skippedCount()),
		slog.String("by_mode", formatTally(timing.tally.byMode)),
		slog.String("unresolved_target_by_type", formatTally(timing.tally.unresolved)),
		slog.String("unresolved_source_by_type", formatTally(timing.tally.unresolvedSource)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
