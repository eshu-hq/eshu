// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// canonicalPhaseObservabilityCoverageEdge names the observability COVERS edge
// projection phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseObservabilityCoverageEdge = "observability_coverage_edge"

// observabilityCoverageEdgeLabel is the bounded entity-label tag for the COVERS
// edge statement metadata and the edge-projection counter dimension.
const observabilityCoverageEdgeLabel = "AWS_COVERS"

// observabilityCoverageSignalVocabulary is the closed set of coverage signals
// the COVERS projection is allowed to turn into an AWS_COVERS_<signal>
// relationship type. It is the single enforcement point for the schema-surface
// contract: a token outside this set is rejected rather than interpolated into a
// new relationship type, so a deviating or adversarial upstream signal can never
// silently fabricate AWS_COVERS_<token>. The members mirror the reducer's
// coverageSignal* vocabulary; the duplication is intentional because the cypher
// writer owns the relationship-type position and must not depend on reducer
// internals.
var observabilityCoverageSignalVocabulary = map[string]struct{}{
	"alarm":           {},
	"composite_alarm": {},
	"dashboard":       {},
	"log_group":       {},
	"trace_sampling":  {},
}

// canonicalObservabilityCoverageEdgeUpsertCypherFormat batches COVERS edge
// upserts between two already-materialized CloudResource nodes: the
// observability object (alarm/dashboard/log group/X-Ray) and the monitored
// resource it covers. The Cypher relationship type is a sanitized static token
// derived from the coverage signal (AWS_COVERS_<signal>). Keeping that token out
// of a relationship MERGE property map is required for NornicDB to use its
// relationship hot path (#805 §5.3: a property-keyed relationship MERGE timed
// out at 20s vs 0–1ms for the static token) while preserving one edge per
// (observability uid, coverage signal, target uid). Two MATCHes precede the
// MERGE so a row whose observability or target node is absent produces no edge
// and no fabricated node.
const canonicalObservabilityCoverageEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (obs:CloudResource {uid: row.observability_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (obs)-[rel:%s]->(target)
SET rel.coverage_signal = row.coverage_signal,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractObservabilityCoverageEdgesCypher removes the reducer-owned COVERS edges
// for a set of scopes before a fresh generation reprojects them. COVERS writes
// use signal-specific Cypher relationship types, so the retract intentionally
// matches any CloudResource relationship and then scopes by the edge properties
// this reducer owns. The scope predicate filters on the edge's own scope_id, not
// the endpoint node's: CloudResource nodes are cross-scope canonical and carry
// no scope_id, so a node-scoped predicate would make the retract a silent no-op
// that leaks stale edges across generations.
const retractObservabilityCoverageEdgesCypher = `MATCH (obs:CloudResource)-[rel]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractObservabilityCoverageEdgesByUIDsCypher is the ledger-anchored
// counterpart of retractObservabilityCoverageEdgesCypher: it enumerates source
// (observability) CloudResource uids from $source_uids instead of scanning
// the whole :CloudResource label. The inline `{uid: suid}` on the MATCH seeds
// the CloudResource.uid index so the delete walks only the ledger-enumerated
// observability node's outgoing adjacency. A bare `[rel]` is safe here: COVERS
// edges use a signal-specific Cypher relationship type
// (AWS_COVERS_<signal>) drawn from a closed vocabulary, but enumerating every
// member would still require keeping this Cypher in lockstep with
// observabilityCoverageSignalVocabulary; the `evidence_source` predicate
// already scopes the delete to only this writer's edges, so a bare `[rel]`
// cannot reach an edge owned by a different writer.
const retractObservabilityCoverageEdgesByUIDsCypher = `UNWIND $source_uids AS suid
MATCH (obs:CloudResource {uid: suid})-[rel]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// observabilityCoverageEdgeRetractUIDBatchSize bounds the number of source
// uids UNWOUND per anchored-retract statement.
const observabilityCoverageEdgeRetractUIDBatchSize = 500

// ObservabilityCoverageEdgeWriter materializes resolved observability coverage
// decisions into canonical COVERS edges between CloudResource nodes. It
// satisfies the reducer-owned coverage-edge-writer consumer interface and writes
// through the backend-neutral Executor seam.
type ObservabilityCoverageEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewObservabilityCoverageEdgeWriter returns an ObservabilityCoverageEdgeWriter
// backed by the given Executor. A batchSize of 0 or less uses DefaultBatchSize
// (500).
func NewObservabilityCoverageEdgeWriter(executor Executor, batchSize int) *ObservabilityCoverageEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &ObservabilityCoverageEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteObservabilityCoverageEdges upserts COVERS edges for the given resolved
// rows using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (observability_uid, coverage_signal, target_uid) converges on one edge across
// batches, retries, and generations, and a missing endpoint is a no-op rather
// than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. The resolution layer does not carry these reducer-scoped
// fields, so the writer is the single place that stamps them; rel.scope_id is
// what the prior-generation retract filters on (CloudResource nodes carry no
// scope_id), so omitting it would make scope-scoped retract a silent no-op.
func (w *ObservabilityCoverageEdgeWriter) WriteObservabilityCoverageEdges(
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
		return fmt.Errorf("observability coverage edge writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	cypherTypes := make([]string, 0, len(rows))
	for _, row := range rows {
		cypherType, err := canonicalObservabilityCoverageCypherType(row)
		if err != nil {
			return err
		}
		cloned := make(map[string]any, len(row)+3)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["scope_id"] = scopeID
		cloned["generation_id"] = generationID
		cloned["evidence_source"] = evidenceSource
		if _, exists := grouped[cypherType]; !exists {
			cypherTypes = append(cypherTypes, cypherType)
		}
		grouped[cypherType] = append(grouped[cypherType], cloned)
	}
	sort.Strings(cypherTypes)

	var stmts []Statement
	for _, cypherType := range cypherTypes {
		cypher := fmt.Sprintf(canonicalObservabilityCoverageEdgeUpsertCypherFormat, cypherType)
		batches := buildBatchedStatements(cypher, grouped[cypherType], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseObservabilityCoverageEdge
			batches[index].Parameters[StatementMetadataEntityLabelKey] = observabilityCoverageEdgeLabel
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"edge=%s type=%s rows=%d",
				observabilityCoverageEdgeLabel,
				cypherType,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}

	return w.dispatch(ctx, stmts)
}

// canonicalObservabilityCoverageCypherType validates the coverage signal and
// returns the sanitized static relationship token (AWS_COVERS_<signal>). The
// signal must be a member of the closed coverage vocabulary (alarm,
// composite_alarm, dashboard, log_group, trace_sampling); membership is checked
// against observabilityCoverageSignalVocabulary, not just the character class, so
// a charset-safe but out-of-vocabulary token (or injected text) can never reach
// the relationship-type position, which is interpolated into Cypher and cannot
// be parameterized. The character-class screen runs first to keep the error for
// unsafe input precise before the allowlist check.
func canonicalObservabilityCoverageCypherType(row map[string]any) (string, error) {
	raw, ok := row["coverage_signal"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("observability coverage_signal must be a non-empty string")
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("observability coverage_signal %q contains unsupported character %q", raw, ch)
	}
	if _, ok := observabilityCoverageSignalVocabulary[raw]; !ok {
		return "", fmt.Errorf("observability coverage_signal %q is outside the closed coverage vocabulary", raw)
	}
	return observabilityCoverageEdgeLabel + "_" + raw, nil
}

// RetractObservabilityCoverageEdges removes this reducer's COVERS edges for the
// given scopes before a fresh generation reprojects them. It is a no-op for an
// empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches CloudResource nodes.
func (w *ObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("observability coverage edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractObservabilityCoverageEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseObservabilityCoverageEdge,
			StatementMetadataEntityLabelKey: observabilityCoverageEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				observabilityCoverageEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// RetractObservabilityCoverageEdgesByUIDs removes this reducer's COVERS edges
// for the given scopes, enumerating source (observability) CloudResource uids
// from the projected-source ledger instead of scanning the whole
// :CloudResource label. It is a no-op for an empty uid set.
func (w *ObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdgesByUIDs(
	ctx context.Context,
	sourceUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(sourceUIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("observability coverage edge writer executor is required")
	}
	batches := chunkStrings(sourceUIDs, observabilityCoverageEdgeRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches))
	for _, batch := range batches {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractObservabilityCoverageEdgesByUIDsCypher,
			Parameters: map[string]any{
				"source_uids":                   batch,
				"scope_ids":                     scopeIDs,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       canonicalPhaseObservabilityCoverageEdge,
				StatementMetadataEntityLabelKey: observabilityCoverageEdgeLabel,
				StatementMetadataSummaryKey: fmt.Sprintf(
					"edge=%s retract_by_uids scopes=%d uids=%d",
					observabilityCoverageEdgeLabel,
					len(scopeIDs),
					len(batch),
				),
			},
		})
	}
	return w.dispatchRetract(ctx, stmts)
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *ObservabilityCoverageEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract routes anchored-retract statements through sequential
// Execute calls, never ExecuteGroup, for the same NornicDB bolt-driver reason
// documented on CloudResourceEdgeWriter.dispatchRetract.
func (w *ObservabilityCoverageEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
