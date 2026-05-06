// Package cypher provides backend-neutral Cypher write contracts, planners, and
// writers for Eshu's canonical graph.
//
// The package owns source-local projection writers, canonical node and edge
// writers, semantic entity writers, statement metadata, retry and timeout
// wrappers, and write instrumentation. Backend-specific driver and session
// adapters belong in narrower storage packages or runtime wiring.
package cypher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

const (
	// DefaultBatchSize is the default number of records per batch when writing Cypher.
	DefaultBatchSize = 500

	upsertNodeCypher = `MERGE (n:SourceLocalRecord {scope_id: $scope_id, generation_id: $generation_id, record_id: $record_id})
SET n.source_system = $source_system,
    n.kind = $kind,
    n.attributes_json = $attributes_json,
    n.deleted = false`

	deleteNodeCypher = `MATCH (n:SourceLocalRecord {scope_id: $scope_id, generation_id: $generation_id, record_id: $record_id})
DELETE n`

	batchUpsertNodeCypher = `UNWIND $rows AS row
MERGE (n:SourceLocalRecord {scope_id: row.scope_id, generation_id: row.generation_id, record_id: row.record_id})
SET n.source_system = row.source_system,
    n.kind = row.kind,
    n.attributes_json = row.attributes_json,
    n.deleted = false`

	batchDeleteNodeCypher = `UNWIND $rows AS row
MATCH (n:SourceLocalRecord {scope_id: row.scope_id, generation_id: row.generation_id, record_id: row.record_id})
DELETE n`
)

// Operation identifies the supported source-local Cypher write type.
type Operation string

const (
	// OperationUpsertNode writes or refreshes one source-local node.
	OperationUpsertNode Operation = "upsert_node"
	// OperationDeleteNode removes one source-local node tombstoned by the source.
	OperationDeleteNode Operation = "delete_node"
)

// Statement captures one executable Cypher statement.
type Statement struct {
	Operation  Operation
	Cypher     string
	Parameters map[string]any
}

// Plan is the deterministic source-local write plan for one materialization.
type Plan struct {
	ScopeID      string
	GenerationID string
	Statements   []Statement
}

// Executor executes one Cypher statement.
type Executor interface {
	Execute(context.Context, Statement) error
}

// GroupExecutor executes multiple Cypher statements in a single atomic
// transaction. Implementations should retry on transient errors (deadlock,
// leader switch). If the executor does not support grouping, callers fall
// back to sequential Execute calls.
type GroupExecutor interface {
	ExecuteGroup(ctx context.Context, stmts []Statement) error
}

// PhaseGroupExecutor executes a bounded batch of statements for one canonical
// write phase. Unlike GroupExecutor, callers should not assume the entire
// materialization is atomic across phases.
type PhaseGroupExecutor interface {
	ExecutePhaseGroup(ctx context.Context, stmts []Statement) error
}

// Adapter writes source-local graph records through an Executor.
type Adapter struct {
	Executor  Executor
	BatchSize int
}

// Write builds and executes the source-local write plan for one materialization.
func (a Adapter) Write(ctx context.Context, materialization graph.Materialization) (graph.Result, error) {
	plan, err := BuildPlan(materialization)
	if err != nil {
		return graph.Result{}, err
	}

	if a.Executor == nil {
		if len(plan.Statements) == 0 {
			return resultFor(materialization), nil
		}
		return graph.Result{}, fmt.Errorf("cypher executor is required when source-local statements are present")
	}

	// Separate statements by operation type and collect rows
	var upsertRows []map[string]any
	var deleteRows []map[string]any

	for i := range plan.Statements {
		stmt := plan.Statements[i]
		switch stmt.Operation {
		case OperationUpsertNode:
			upsertRows = append(upsertRows, stmt.Parameters)
		case OperationDeleteNode:
			deleteRows = append(deleteRows, stmt.Parameters)
		}
	}

	// Execute upserts in batches
	if len(upsertRows) > 0 {
		if err := a.executeBatched(ctx, OperationUpsertNode, batchUpsertNodeCypher, upsertRows); err != nil {
			return graph.Result{}, fmt.Errorf("execute batched upserts: %w", err)
		}
	}

	// Execute deletes in batches
	if len(deleteRows) > 0 {
		if err := a.executeBatched(ctx, OperationDeleteNode, batchDeleteNodeCypher, deleteRows); err != nil {
			return graph.Result{}, fmt.Errorf("execute batched deletes: %w", err)
		}
	}

	return resultFor(materialization), nil
}

// executeBatched executes batched operations using UNWIND.
func (a Adapter) executeBatched(ctx context.Context, op Operation, cypher string, rows []map[string]any) error {
	batchSize := a.batchSize()
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := a.Executor.Execute(ctx, Statement{
			Operation:  op,
			Cypher:     cypher,
			Parameters: map[string]any{"rows": rows[start:end]},
		}); err != nil {
			return err
		}
	}
	return nil
}

// batchSize returns the configured batch size or the default if unset.
func (a Adapter) batchSize() int {
	if a.BatchSize <= 0 {
		return DefaultBatchSize
	}
	return a.BatchSize
}

// BuildPlan converts a source-local graph materialization into Cypher statements.
func BuildPlan(materialization graph.Materialization) (Plan, error) {
	if err := validateMaterialization(materialization); err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ScopeID:      materialization.ScopeID,
		GenerationID: materialization.GenerationID,
	}

	for i := range materialization.Records {
		record := materialization.Records[i].Clone()
		statement, err := buildStatement(materialization, record, i)
		if err != nil {
			return Plan{}, err
		}
		if statement.Operation == "" {
			continue
		}
		plan.Statements = append(plan.Statements, statement)
	}

	return plan, nil
}

func buildStatement(materialization graph.Materialization, record graph.Record, index int) (Statement, error) {
	if err := validateRecord(record, index); err != nil {
		return Statement{}, err
	}

	if record.Deleted {
		return Statement{
			Operation:  OperationDeleteNode,
			Cypher:     deleteNodeCypher,
			Parameters: deleteParameters(materialization, record),
		}, nil
	}

	if err := validateUpsertRecord(record, index); err != nil {
		return Statement{}, err
	}

	parameters, err := upsertParameters(materialization, record)
	if err != nil {
		return Statement{}, fmt.Errorf("build upsert parameters: %w", err)
	}

	return Statement{
		Operation:  OperationUpsertNode,
		Cypher:     upsertNodeCypher,
		Parameters: parameters,
	}, nil
}

func validateMaterialization(materialization graph.Materialization) error {
	if strings.TrimSpace(materialization.ScopeID) == "" {
		return fmt.Errorf("scope_id must not be blank")
	}
	if strings.TrimSpace(materialization.GenerationID) == "" {
		return fmt.Errorf("generation_id must not be blank")
	}
	if strings.TrimSpace(materialization.SourceSystem) == "" {
		return fmt.Errorf("source_system must not be blank")
	}

	return nil
}

func validateRecord(record graph.Record, index int) error {
	if strings.TrimSpace(record.RecordID) == "" {
		return fmt.Errorf("record %d record_id must not be blank", index)
	}

	return nil
}

func validateUpsertRecord(record graph.Record, index int) error {
	if strings.TrimSpace(record.Kind) == "" {
		return fmt.Errorf("record %d kind must not be blank for source-local upsert", index)
	}

	return nil
}

func upsertParameters(materialization graph.Materialization, record graph.Record) (map[string]any, error) {
	attributes := cloneStringMap(record.Attributes)
	if attributes == nil {
		attributes = map[string]string{}
	}
	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return nil, fmt.Errorf("marshal attributes json: %w", err)
	}

	return map[string]any{
		"scope_id":        materialization.ScopeID,
		"generation_id":   materialization.GenerationID,
		"source_system":   materialization.SourceSystem,
		"record_id":       record.RecordID,
		"kind":            record.Kind,
		"attributes_json": string(attributesJSON),
	}, nil
}

func deleteParameters(materialization graph.Materialization, record graph.Record) map[string]any {
	return map[string]any{
		"scope_id":      materialization.ScopeID,
		"generation_id": materialization.GenerationID,
		"record_id":     record.RecordID,
	}
}

func resultFor(materialization graph.Materialization) graph.Result {
	result := graph.Result{
		ScopeID:      materialization.ScopeID,
		GenerationID: materialization.GenerationID,
		RecordCount:  len(materialization.Records),
	}
	for i := range materialization.Records {
		if materialization.Records[i].Deleted {
			result.DeletedCount++
		}
	}

	return result
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}

	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}

	return cloned
}
