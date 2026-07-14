// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func workloadCloudRelationshipMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainWorkloadCloudRelationshipMaterialization,
		Summary: "project exact workload-anchored CloudResource facts into canonical USES graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "workload_cloud_relationship_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

const workloadCloudRelationshipEvidenceSource = "reducer/workload-cloud-relationship"

// WorkloadCloudRelationshipEdgeWriter persists and retracts canonical USES
// edges from workload instances to cloud resources. Implementations MUST match
// existing endpoints and MUST NOT fabricate WorkloadInstance or CloudResource
// nodes.
type WorkloadCloudRelationshipEdgeWriter interface {
	WriteWorkloadCloudRelationshipEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractWorkloadCloudRelationshipEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// WorkloadCloudRelationshipMaterializationHandler projects exact workload
// anchors on aws_resource facts into canonical USES edges. Service-name-only
// and ambiguous anchors remain candidate evidence for query surfaces.
type WorkloadCloudRelationshipMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           WorkloadCloudRelationshipEdgeWriter
	ReadinessLookup      GraphProjectionReadinessLookup
	PriorGenerationCheck PriorGenerationCheck
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// when an aws_resource fact is quarantined as input_invalid during workload
	// anchor extraction. Optional: a nil pointer skips the counter (the
	// structured per-fact error log still emits).
	Instruments *telemetry.Instruments
}

func (h WorkloadCloudRelationshipMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainWorkloadCloudRelationshipMaterialization {
		return Result{}, fmt.Errorf(
			"workload cloud relationship materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("workload cloud relationship fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("workload cloud relationship edge writer is required")
	}
	if !h.endpointsReady(intent) {
		return Result{}, workloadCloudRelationshipNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.AWSResourceFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for workload cloud relationship materialization: %w", err)
	}

	rows, tally, quarantined, err := ExtractWorkloadCloudRelationshipRows(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while every valid workload anchor still
	// projects its edge below.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainWorkloadCloudRelationshipMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if err := h.EdgeWriter.RetractWorkloadCloudRelationshipEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			workloadCloudRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract workload cloud USES edges: %w", err)
		}
	}
	if len(rows) > 0 {
		if err := h.EdgeWriter.WriteWorkloadCloudRelationshipEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			workloadCloudRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write workload cloud USES edges: %w", err)
		}
	}

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainWorkloadCloudRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d workload cloud USES edge(s) from %d aws resource fact(s); %d candidate(s) skipped; %d input_invalid fact(s) quarantined",
			len(rows),
			len(envelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h WorkloadCloudRelationshipMaterializationHandler) endpointsReady(intent Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	cloudKey := GraphProjectionPhaseKey{
		ScopeID:          intent.ScopeID,
		AcceptanceUnitID: "aws_resource_materialization:" + intent.ScopeID,
		SourceRunID:      intent.GenerationID,
		GenerationID:     intent.GenerationID,
		Keyspace:         GraphProjectionKeyspaceCloudResourceUID,
	}
	if len(intent.EntityKeys) > 0 && intent.EntityKeys[0] != "" {
		cloudKey.AcceptanceUnitID = intent.EntityKeys[0]
	}
	states := []struct {
		key   GraphProjectionPhaseKey
		phase GraphProjectionPhase
	}{
		{key: cloudKey, phase: GraphProjectionPhaseCanonicalNodesCommitted},
	}
	for _, state := range states {
		state.key.GenerationID = intent.GenerationID
		state.key.SourceRunID = intent.GenerationID
		if err := state.key.Validate(); err != nil {
			return false
		}
		ready, found := h.ReadinessLookup(state.key, state.phase)
		if !found || !ready {
			return false
		}
	}
	return true
}

func (h WorkloadCloudRelationshipMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for workload cloud relationship retract: %w", err)
	}
	return !hasPrior, nil
}

type workloadCloudRelationshipNotReadyError struct {
	scopeID      string
	generationID string
}

func (e workloadCloudRelationshipNotReadyError) Error() string {
	return fmt.Sprintf(
		"cloud resource nodes not committed for workload cloud relationship scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (workloadCloudRelationshipNotReadyError) Retryable() bool { return true }

func (workloadCloudRelationshipNotReadyError) FailureClass() string {
	return "workload_cloud_relationship_nodes_not_ready"
}

const (
	workloadCloudRelationshipRelationshipType = string(edgetype.Uses)
	workloadCloudRelationshipModeWorkload     = "explicit_workload_anchor"

	workloadCloudRelationshipSkipMissingWorkloadAnchor = "missing_workload_anchor"
	workloadCloudRelationshipSkipAmbiguousAnchor       = "ambiguous_anchor"
	workloadCloudRelationshipSkipMissingResource       = "missing_cloud_resource_identity"
	workloadCloudRelationshipSkipMissingEnvironment    = "missing_environment"
)

type workloadCloudRelationshipTally struct {
	skipped map[string]int
}

func newWorkloadCloudRelationshipTally() workloadCloudRelationshipTally {
	return workloadCloudRelationshipTally{skipped: make(map[string]int)}
}

func (t workloadCloudRelationshipTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// ExtractWorkloadCloudRelationshipRows builds canonical USES edge rows only for
// aws_resource facts with exactly one explicit workload anchor. Service-name-only
// anchors stay candidate evidence and are not promoted to graph truth. Each
// aws_resource fact is decoded through the factschema seam, so a payload missing
// a required identity field dead-letters (input_invalid); the workload anchor and
// environment (service-specific fields) are read from the decoded struct's
// untyped Attributes pass-through.
func ExtractWorkloadCloudRelationshipRows(envelopes []facts.Envelope) ([]map[string]any, workloadCloudRelationshipTally, []quarantinedFact, error) {
	tally := newWorkloadCloudRelationshipTally()
	if len(envelopes) == 0 {
		return nil, tally, nil, nil
	}

	type edgeKey struct {
		workload    string
		environment string
		cloud       string
	}
	var quarantined []quarantinedFact
	seen := make(map[edgeKey]struct{}, len(envelopes))
	rows := make([]map[string]any, 0, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		// The workload anchor and environment tags are typed through
		// awsv1.DecodeResourceAnchorAttributes (issue #4631): a present-but-
		// wrong-typed value is a decode failure the extractor must quarantine
		// rather than silently coerce, since a malformed anchor could otherwise
		// silently mis-key or mis-drop a workload-to-cloud-resource USES edge.
		workloadAnchor, attrErr := awsv1.DecodeResourceAnchorAttributes(resource)
		if attrErr != nil {
			quarantined = append(quarantined, quarantinedAttributeShapeFact(env, attrErr))
			continue
		}
		workloadIDs := uniqueSortedStrings(workloadAnchor.WorkloadIDs)
		switch len(workloadIDs) {
		case 0:
			tally.skipped[workloadCloudRelationshipSkipMissingWorkloadAnchor]++
			continue
		case 1:
		default:
			tally.skipped[workloadCloudRelationshipSkipAmbiguousAnchor]++
			continue
		}

		// Reuse the already-decoded resource to derive the uid rather than calling
		// cloudResourceNodeRow(env), which would decode the same envelope a second
		// time on this hot path. cloudResourceUIDForResource shares the exact
		// identity rules cloudResourceNodeRow uses.
		cloudUID, ok := cloudResourceUIDForResource(resource)
		if !ok {
			tally.skipped[workloadCloudRelationshipSkipMissingResource]++
			continue
		}
		environment := workloadAnchor.Environment
		if environment == "" {
			tally.skipped[workloadCloudRelationshipSkipMissingEnvironment]++
			continue
		}
		key := edgeKey{workload: workloadIDs[0], environment: environment, cloud: cloudUID}
		if _, dup := seen[key]; dup {
			continue
		}
		anchor, err := cloudResourceServiceAnchorDecisionForPayload(resource)
		if err != nil {
			quarantined = append(quarantined, quarantinedAttributeShapeFact(env, err))
			continue
		}
		seen[key] = struct{}{}
		rows = append(rows, map[string]any{
			"workload_id":           workloadIDs[0],
			"cloud_resource_uid":    cloudUID,
			"relationship_type":     workloadCloudRelationshipRelationshipType,
			"resolution_mode":       workloadCloudRelationshipModeWorkload,
			"environment":           environment,
			"relationship_basis":    "aws_resource_service_anchor",
			"service_anchor_source": anchor.Source,
			"service_anchor_reason": anchor.Reason,
			"source_fact_id":        env.FactID,
			"stable_fact_key":       env.StableFactKey,
			"source_system":         env.SourceRef.SourceSystem,
			"source_record_id":      env.SourceRef.SourceRecordID,
			"collector_kind":        env.CollectorKind,
		})
	}
	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["workload_id"]) + "@" + anyToString(rows[a]["environment"]) + "->" + anyToString(rows[a]["cloud_resource_uid"])
		right := anyToString(rows[b]["workload_id"]) + "@" + anyToString(rows[b]["environment"]) + "->" + anyToString(rows[b]["cloud_resource_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}
