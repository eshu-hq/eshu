// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	"regexp"
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
//
// Drain and DrainVar are set by canonical writers when NornicDB must run a
// retract outside a grouped ExecuteWrite transaction. Unbounded full-refresh
// node retracts (files, removed-files, directories, and per-label entities) set
// Drain=true plus DrainVar so the phase-group executor converts them into a
// bounded drain loop via BuildBoundedRetractDrainCypher. Bounded mixed-phase
// relationship retracts set Drain=true with an empty DrainVar so the executor
// runs them once as standalone autocommit statements before grouped sibling
// upserts.
type Statement struct {
	Operation  Operation
	Cypher     string
	Parameters map[string]any

	// Drain marks this statement for special retract execution on NornicDB.
	// Neo4j and all other backends ignore this field.
	Drain bool

	// DrainVar is the Cypher variable that names the node to delete in an
	// unbounded trailing DETACH DELETE clause (e.g. "f", "d", "n"). It is empty
	// for bounded mixed-phase relationship retracts.
	DrainVar string
}

// allStatementsAreReplaySafe returns true when re-executing every statement in
// stmts, in order, converges on the same graph state as the attempt that
// failed. That is the property the group retry actually needs: a NornicDB
// commit failure rolls the whole transaction back rather than tearing it, so
// the replay starts from the pre-group state and must be free to repeat every
// statement. Empty groups return false because there is nothing to retry.
//
// Two statement shapes qualify. A MERGE-shaped statement converges by
// definition. A predicate-scoped retract does too, but only when its
// predicates are bounded BY the bound parameters rather than by their
// complement: it then deletes or clears whatever matches a key space the
// parameters enumerate, so a second run removes the same set (a no-op when the
// first attempt committed nothing) and never creates or accumulates anything.
// isIdempotentRetractStatement enforces that bound; a retract selecting a
// parameter's complement (`n.generation_id <> $gen`, `NOT (n.path IN
// $paths)`) or naming no parameter at all is refused, because a concurrent
// writer can move rows INTO its match set between attempt and replay.
//
// Everything else keeps the group terminal — a CREATE duplicates on replay and
// an accumulating SET double-applies, which is the safety the MERGE-only gate
// used to buy by refusing every mixed group.
//
// #6176 is why the retract shape had to be named explicitly. The semantic
// writer used to dispatch its retract outside the group, so the group reaching
// this classifier was all-MERGE; folding retract and upsert into one atomic
// transaction made it mixed, and a MERGE-only gate would have turned the
// commit-time UNIQUE conflict that a concurrent canonical writer produces from
// a retried, converging write into a dead-lettered work item.
func allStatementsAreReplaySafe(stmts []Statement) bool {
	if len(stmts) == 0 {
		return false
	}
	for _, s := range stmts {
		if isMergeShapedCypher(s.Cypher) {
			continue
		}
		if isIdempotentRetractStatement(s) {
			continue
		}
		return false
	}
	return true
}

// isMergeShapedCypher reports whether cypher contains a MERGE clause, the
// original and still-primary replay-safety signal for a canonical upsert.
func isMergeShapedCypher(cypher string) bool {
	return strings.Contains(strings.ToUpper(cypher), "MERGE")
}

var (
	// nonIdempotentClausePattern matches the clauses that stop a retract from
	// being a plain delete-by-predicate. CREATE duplicates on replay and SET
	// can accumulate; CALL and FOREACH hide writes this reasoning cannot see;
	// and UNWIND marks the row-driven MATCH ... DELETE shape that no-ops
	// inside a NornicDB managed transaction
	// (docs/public/reference/nornicdb-pitfalls.md), which must never be
	// replayed as though it had applied. MERGE is deliberately absent: a
	// statement containing one is already replay-safe by the MERGE branch, so
	// listing it here would be unreachable.
	nonIdempotentClausePattern = regexp.MustCompile(`(?i)\b(CREATE|SET|UNWIND|CALL|FOREACH)\b`)
	// retractClausePattern matches the clauses a retract uses to remove state:
	// DELETE (with or without DETACH) for nodes and relationships, REMOVE for
	// labels and properties.
	retractClausePattern = regexp.MustCompile(`(?i)\b(DELETE|REMOVE)\b`)
	// leadingMatchPattern anchors the statement on MATCH, so its predicates
	// come from the graph plus bound parameters rather than from rows the
	// group itself produced.
	leadingMatchPattern = regexp.MustCompile(`(?i)^\s*MATCH\b`)
	// relationshipArrowPattern matches the arrow tokens of a Cypher
	// relationship pattern, stripped before the predicate scan below: the `<`
	// and `>` in `-[rel]->` and `<-[e:MATCHES_STATE]-` are pattern syntax, not
	// comparisons, and reading them as comparisons would fail-close every
	// relationship retract in the repository.
	relationshipArrowPattern = regexp.MustCompile(`<-\[[^\]]*\]-|-\[[^\]]*\]->|<--|-->|<-|->`)
	// openEndedPredicatePattern matches the comparison forms selecting the
	// COMPLEMENT of what the bound parameters name: `<>`, `!=`, the ordering
	// operators, and any NOT (covering `NOT (n.path IN $paths)` and a bare
	// `n.x IS NOT NULL`). Such a predicate has no fixed key space -- rows a
	// concurrent writer commits outside the parameter values fall INTO range
	// -- so its match set grows between a failed attempt and its replay.
	openEndedPredicatePattern = regexp.MustCompile(`(?i)(<>|!=|<=|>=|<|>|\bNOT\b)`)
	// boundParameterPattern matches a Cypher parameter reference. A retract
	// that names none is scoped by graph state alone, which is the same
	// unbounded-match-set problem in its most extreme form.
	boundParameterPattern = regexp.MustCompile(`\$\w+`)
)

// isIdempotentRetractStatement reports whether stmt is a predicate-scoped
// retract whose replay removes the same parameter-bound set. It requires all
// four of: the statement declaring itself a retract through
// OperationCanonicalRetract, Cypher that opens on MATCH, Cypher whose only
// write clauses are DELETE or REMOVE, and predicates that are bounded by the
// bound parameters rather than by their complement. The clause checks are
// word-bounded so a property name such as n.offset cannot be read as a SET
// clause and fail-close a legitimate retract.
func isIdempotentRetractStatement(stmt Statement) bool {
	if stmt.Operation != OperationCanonicalRetract {
		return false
	}
	if !leadingMatchPattern.MatchString(stmt.Cypher) {
		return false
	}
	if nonIdempotentClausePattern.MatchString(stmt.Cypher) {
		return false
	}
	if !hasParameterBoundedPredicate(stmt.Cypher) {
		return false
	}
	return retractClausePattern.MatchString(stmt.Cypher)
}

// hasParameterBoundedPredicate reports whether every comparison in cypher
// narrows the match set to what the bound parameters name rather than to their
// complement. It makes the "removes the same parameter-bound set" premise true
// rather than merely stated.
//
// Positive membership -- `n.path IN $file_paths`, `n.repo_id = $repo_id`, an
// inline `{id: $repo_id}` map, an equality against an immutable literal --
// confines the delete to a key space the parameters enumerate. Nothing a
// concurrent writer commits can enlarge that space, so the replay after a
// rolled-back attempt sees the same candidates.
//
// An open-ended predicate inverts that. `p.generation_id <> $generation_id`
// (canonicalNodeRetractParametersCypher) matches every generation EXCEPT this
// writer's, so a Parameter a concurrent writer commits on a different
// generation between the failed attempt and the replay is newly in range and
// gets deleted -- a node the first attempt never saw. `NOT (d.path IN
// $directory_paths)` and a parameterless `WHERE n.stale` are the same class.
// Refusing them keeps the group terminal and sends the work item to
// dead-letter redrive instead of silently deleting another writer's rows.
//
// The check is syntactic and fail-closed: it proves a predicate is bounded,
// not that an unbounded one is unsafe in a given deployment. A refused group
// loses a retry it might not have needed, which costs time; accepting one
// whose match set grows under concurrency costs graph truth.
func hasParameterBoundedPredicate(cypher string) bool {
	if !boundParameterPattern.MatchString(cypher) {
		return false
	}
	return !openEndedPredicatePattern.MatchString(
		relationshipArrowPattern.ReplaceAllString(cypher, " "),
	)
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

// ProbeExecutor is an optional capability an Executor implementation can add
// to answer whether a read-only Cypher statement would match at least one row,
// without mutating the graph. It exists so a caller planning an expensive
// bounded-cost retract (a NornicDB DELETE whose cost tracks store size rather
// than rows deleted -- see docs/public/reference/nornicdb-pitfalls.md) can
// probe first with the identical MATCH/WHERE shape and skip the delete when it
// would remove zero rows (#5998).
//
// Every wrapper in this package that forwards GroupExecutor to its Inner MUST
// forward ProbeExecutor the same way, or the capability is silently swallowed
// partway down the chain and the calling optimization becomes a permanent
// no-op. A wrapper that deliberately hides GroupExecutor from its Inner (for
// example ExecuteOnlyExecutor) MUST likewise not implement ProbeExecutor, so a
// type assertion for either capability fails identically through that seam.
//
// Callers MUST treat the absence of this interface, and any error ExecuteProbe
// returns, as "unknown" -- never as "zero rows" -- and fail safe by running the
// paired mutating statement unconditionally. A skipped delete can leave stale
// graph state; a redundant delete only costs time.
type ProbeExecutor interface {
	// ExecuteProbe runs stmt as a read-only query and reports whether it
	// matched at least one row. found is meaningful only when err is nil.
	ExecuteProbe(ctx context.Context, stmt Statement) (bool, error)
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
