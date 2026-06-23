package cypher

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
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

// IncidentRoutingEvidenceWriter materializes exact PagerDuty incident-routing
// evidence into reducer-owned graph evidence nodes and relationships.
type IncidentRoutingEvidenceWriter struct {
	executor  Executor
	batchSize int
}

// NewIncidentRoutingEvidenceWriter returns an IncidentRoutingEvidenceWriter
// backed by the given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewIncidentRoutingEvidenceWriter(executor Executor, batchSize int) *IncidentRoutingEvidenceWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &IncidentRoutingEvidenceWriter{executor: executor, batchSize: batchSize}
}

// WriteIncidentRoutingEvidence upserts reducer-owned incident-routing evidence
// nodes and relationships. Rows are grouped by closed routing slot so the Cypher
// relationship type is static and cannot be data-injected.
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
		cloned := cloneRowWith(row, map[string]any{
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

	var stmts []Statement
	for _, slot := range slots {
		relType := incidentRoutingSlotRelationshipTypes[slot]
		cypher := fmt.Sprintf(canonicalIncidentRoutingEvidenceUpsertCypherFormat, relType)
		batches := buildBatchedStatements(cypher, grouped[slot], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseIncidentRoutingEvidence
			batches[index].Parameters[StatementMetadataEntityLabelKey] = incidentRoutingEvidenceLabel
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
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
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractIncidentRoutingEvidenceCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseIncidentRoutingEvidence,
			StatementMetadataEntityLabelKey: incidentRoutingEvidenceLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				incidentRoutingEvidenceLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}
	return w.dispatch(ctx, []Statement{stmt})
}

func validateIncidentRoutingSlot(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "slot", incidentRoutingSlotVocabulary, "incident routing slot")
}

func (w *IncidentRoutingEvidenceWriter) dispatch(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
