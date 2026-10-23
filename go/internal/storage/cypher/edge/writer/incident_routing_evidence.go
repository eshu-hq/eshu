// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	canonicalPhaseIncidentRoutingEvidence = "incident_routing_evidence"
	incidentRoutingEvidenceLabel          = "IncidentRoutingEvidence"
)

var incidentRoutingSlotVocabulary = map[string]struct{}{
	"intended_routing": {},
	"applied_routing":  {},
	"live_routing":     {},
}

var incidentRoutingSlotRelationshipTypes = map[string]string{
	"intended_routing": string(edgetype.HasIntendedRouting),
	"applied_routing":  string(edgetype.HasAppliedRouting),
	"live_routing":     string(edgetype.HasLiveRouting),
}

// canonicalIncidentRoutingEvidenceIncidentUpsertCypher commits the incident
// anchor nodes BEFORE the edge group runs. The edge statements re-MERGE the
// same incident uid once per slot, so without this phase the group would
// need read-your-writes across statements inside one transaction to see the
// first statement's create -- which NornicDB does not provide for the write
// path (its documented gap covers the whole-materialization transaction
// shape, and the phase-group executor's cross-chunk safety rests on
// disjoint MERGE keys). Committed first, every later MERGE only ever
// matches; the edge group then shares the key safely because nothing in it
// creates.
const canonicalIncidentRoutingEvidenceIncidentUpsertCypher = `UNWIND $incidents AS row
MERGE (incident:IncidentRoutingEvidence {uid: row.incident_uid})
ON CREATE SET incident.node_kind = 'incident'
SET incident.id = row.incident_uid,
    incident.node_kind = 'incident',
    incident.provider = row.provider,
    incident.provider_incident_id = row.provider_incident_id,
    incident.service_id = row.service_id,
    incident.service_url = row.service_url,
    incident.service_name_hash = row.service_name_hash,
    incident.incident_fact_id = row.incident_fact_id,
    incident.scope_id = row.scope_id,
    incident.generation_id = row.generation_id,
    incident.evidence_source = row.evidence_source`

const canonicalIncidentRoutingEvidenceUpsertCypherFormat = `UNWIND $rows AS row
MERGE (incident:IncidentRoutingEvidence {uid: row.incident_uid})
ON CREATE SET incident.node_kind = 'incident'
SET incident.id = row.incident_uid,
    incident.node_kind = 'incident',
    incident.provider = row.provider,
    incident.provider_incident_id = row.provider_incident_id,
    incident.service_id = row.service_id,
    incident.service_url = row.service_url,
    incident.service_name_hash = row.service_name_hash,
    incident.incident_fact_id = row.incident_fact_id,
    incident.scope_id = row.scope_id,
    incident.generation_id = row.generation_id,
    incident.evidence_source = row.evidence_source
MERGE (routing:IncidentRoutingEvidence {uid: row.uid})
ON CREATE SET routing.node_kind = 'routing'
SET routing.id = row.uid,
    routing.node_kind = 'routing',
    routing.slot = row.slot,
    routing.source_class = row.source_class,
    routing.truth_label = row.truth_label,
    routing.provider = row.provider,
    routing.provider_incident_id = row.provider_incident_id,
    routing.service_id = row.service_id,
    routing.service_name_hash = row.service_name_hash,
    routing.evidence_kind = row.evidence_kind,
    routing.evidence_id = row.evidence_id,
    routing.source_kind = row.source_kind,
    routing.resource_class = row.resource_class,
    routing.provider_object_id = row.provider_object_id,
    routing.repo_id = row.repo_id,
    routing.relative_path = row.relative_path,
    routing.terraform_state_address = row.terraform_state_address,
    routing.provider_address = row.provider_address,
    routing.module_address = row.module_address,
    routing.state_generation_id = row.state_generation_id,
    routing.declared_match_state = row.declared_match_state,
    routing.redaction_state = row.redaction_state,
    routing.scope_id = row.scope_id,
    routing.generation_id = row.generation_id,
    routing.evidence_source = row.evidence_source
MERGE (incident)-[rel:%s]->(routing)
SET rel.slot = row.slot,
    rel.source_class = row.source_class,
    rel.truth_label = row.truth_label,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const retractIncidentRoutingEvidenceCypher = `MATCH (n:IncidentRoutingEvidence)
WHERE n.scope_id IN $scope_ids
  AND n.evidence_source = $evidence_source
DETACH DELETE n`

// Registry-contract holders for the #6228 materialized-edge retract registry
// (docs/internal/design/6228-materialized-edge-retract-registry-contract.md).
// The registry in go/internal/storage/cypher/edge/materialized must hold the
// production consts themselves, never a copy of their text, and derive the
// family's edge types from the vocabulary maps below rather than a copied
// literal.
const (
	// CanonicalIncidentRoutingEvidenceUpsertCypherFormat is the production write template.
	CanonicalIncidentRoutingEvidenceUpsertCypherFormat = canonicalIncidentRoutingEvidenceUpsertCypherFormat
	// CanonicalIncidentRoutingEvidenceIncidentUpsertCypher is the production
	// incident-anchor commit the edge group depends on.
	CanonicalIncidentRoutingEvidenceIncidentUpsertCypher = canonicalIncidentRoutingEvidenceIncidentUpsertCypher
	// RetractIncidentRoutingEvidenceCypher is the production full-scope retract.
	RetractIncidentRoutingEvidenceCypher = retractIncidentRoutingEvidenceCypher
)

// IncidentRoutingEvidenceSlotVocabulary returns a copy of the closed routing
// slots the writer accepts, following the CodeCallMaterializedEdgeTypes
// copy-on-read precedent so a caller cannot mutate the writer's vocabulary.
func IncidentRoutingEvidenceSlotVocabulary() map[string]struct{} {
	out := make(map[string]struct{}, len(incidentRoutingSlotVocabulary))
	for slot := range incidentRoutingSlotVocabulary {
		out[slot] = struct{}{}
	}
	return out
}

// IncidentRoutingEvidenceRelationshipTypes returns a copy of the slot to graph
// relationship type map the writer interpolates into the edge-type position,
// so the registry derives the family's types from the same map the write path
// screens each row against.
func IncidentRoutingEvidenceRelationshipTypes() map[string]string {
	out := make(map[string]string, len(incidentRoutingSlotRelationshipTypes))
	for slot, relType := range incidentRoutingSlotRelationshipTypes {
		out[slot] = relType
	}
	return out
}

// IncidentRoutingEvidenceWriter materializes exact PagerDuty incident-routing
// evidence into reducer-owned graph evidence nodes and relationships.
type IncidentRoutingEvidenceWriter struct {
	executor  sourcecypher.Executor
	batchSize int
}

// NewIncidentRoutingEvidenceWriter returns an IncidentRoutingEvidenceWriter
// backed by the given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewIncidentRoutingEvidenceWriter(executor sourcecypher.Executor, batchSize int) *IncidentRoutingEvidenceWriter {
	if batchSize <= 0 {
		batchSize = sourcecypher.DefaultBatchSize
	}
	return &IncidentRoutingEvidenceWriter{executor: executor, batchSize: batchSize}
}

// WriteIncidentRoutingEvidence upserts reducer-owned incident-routing evidence
// nodes and relationships. Rows are grouped by closed routing slot so the Cypher
// relationship type is static and cannot be data-injected.
//
// The write runs in two phases. Phase one commits the distinct incident
// anchor nodes on their own; phase two upserts the per-slot routing nodes
// and edges in one group. The edge statements re-MERGE the same incident
// uid once per slot, so committing the anchor first is what keeps the group
// safe: NornicDB does not provide read-your-writes across statements inside
// one transaction, and a group that creates the shared anchor would fail
// deterministically with a UNIQUE violation on commit instead of matching
// it. Committed first, every later MERGE only ever matches.
func (w *IncidentRoutingEvidenceWriter) WriteIncidentRoutingEvidence(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("incident routing evidence writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	slots := make([]string, 0, len(rows))
	for _, row := range rows {
		slot, err := validateIncidentRoutingSlot(row)
		if err != nil {
			return err
		}
		cloned := sourcecypher.CloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		})
		if _, exists := grouped[slot]; !exists {
			slots = append(slots, slot)
		}
		grouped[slot] = append(grouped[slot], cloned)
	}
	sort.Strings(slots)

	// Distinct incident anchors in sorted uid order: deterministic across
	// runs, and one row per uid so the anchor commit never shares a MERGE
	// key with itself. Every row of one incident carries the same incident
	// fields (they decode from the same incident record), so first-seen
	// wins without loss.
	incidentUIDs := make([]string, 0, len(rows))
	incidentByUID := make(map[string]map[string]any, len(rows))
	for _, slot := range slots {
		for _, row := range grouped[slot] {
			uid, _ := row["incident_uid"].(string)
			if _, seen := incidentByUID[uid]; seen {
				continue
			}
			incidentByUID[uid] = map[string]any{
				"incident_uid":         uid,
				"provider":             row["provider"],
				"provider_incident_id": row["provider_incident_id"],
				"service_id":           row["service_id"],
				"service_url":          row["service_url"],
				"service_name_hash":    row["service_name_hash"],
				"incident_fact_id":     row["incident_fact_id"],
				"scope_id":             scopeID,
				"generation_id":        generationID,
				"evidence_source":      evidenceSource,
			}
			incidentUIDs = append(incidentUIDs, uid)
		}
	}
	sort.Strings(incidentUIDs)
	incidents := make([]map[string]any, 0, len(incidentUIDs))
	for _, uid := range incidentUIDs {
		incidents = append(incidents, incidentByUID[uid])
	}
	anchor := sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    canonicalIncidentRoutingEvidenceIncidentUpsertCypher,
		Parameters: map[string]any{
			"incidents":                                  incidents,
			sourcecypher.StatementMetadataPhaseKey:       canonicalPhaseIncidentRoutingEvidence,
			sourcecypher.StatementMetadataEntityLabelKey: incidentRoutingEvidenceLabel,
			sourcecypher.StatementMetadataSummaryKey:     fmt.Sprintf("label=%s anchors=%d", incidentRoutingEvidenceLabel, len(incidents)),
		},
	}
	if err := w.executor.Execute(ctx, anchor); err != nil {
		return fmt.Errorf("write incident routing anchor nodes: %w", sourcecypher.WrapRetryableNeo4jError(err))
	}

	var stmts []sourcecypher.Statement
	for _, slot := range slots {
		relType := incidentRoutingSlotRelationshipTypes[slot]
		cypher := fmt.Sprintf(canonicalIncidentRoutingEvidenceUpsertCypherFormat, relType)
		batches := sourcecypher.BuildBatchedStatements(cypher, grouped[slot], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseIncidentRoutingEvidence
			batches[index].Parameters[sourcecypher.StatementMetadataEntityLabelKey] = incidentRoutingEvidenceLabel
			batches[index].Parameters[sourcecypher.StatementMetadataSummaryKey] = fmt.Sprintf(
				"label=%s slot=%s rows=%d",
				incidentRoutingEvidenceLabel,
				slot,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}
	return w.dispatch(ctx, stmts)
}

// RetractIncidentRoutingEvidence removes reducer-owned incident-routing evidence
// nodes for the given scopes before a fresh generation reprojects them.
func (w *IncidentRoutingEvidenceWriter) RetractIncidentRoutingEvidence(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("incident routing evidence writer executor is required")
	}
	stmt := sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    retractIncidentRoutingEvidenceCypher,
		Parameters: map[string]any{
			"scope_ids":                                  scopeIDs,
			"evidence_source":                            evidenceSource,
			sourcecypher.StatementMetadataPhaseKey:       canonicalPhaseIncidentRoutingEvidence,
			sourcecypher.StatementMetadataEntityLabelKey: incidentRoutingEvidenceLabel,
			sourcecypher.StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				incidentRoutingEvidenceLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}
	return w.dispatchRetract(ctx, []sourcecypher.Statement{stmt})
}

func validateIncidentRoutingSlot(row map[string]any) (string, error) {
	return sourcecypher.ValidateStaticGraphToken(row, "slot", incidentRoutingSlotVocabulary, "incident routing slot")
}

func (w *IncidentRoutingEvidenceWriter) dispatch(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if ge, ok := w.executor.(sourcecypher.GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// dispatchRetract runs retract statements sequentially through Execute, each in
// its own auto-commit transaction — never ExecuteGroup. On NornicDB v1.1.11 a
// retract inside a managed transaction can under-apply even as a single
// statement (#4367/#5128/#5146/#5152). RetractIncidentRoutingEvidence routes
// through this so the retract is never batched with a sibling write via
// ExecuteGroup.
func (w *IncidentRoutingEvidenceWriter) dispatchRetract(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
