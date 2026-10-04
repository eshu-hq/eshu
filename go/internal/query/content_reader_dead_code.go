// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// CodeReachabilityIncomingEntityIDs returns dead-code reachability evidence
// from reducer-materialized code_reachability_rows. The lookup is entity-scoped
// across active generations so a library scan can honor rows materialized from
// a service repository that reaches the library symbol; DeadCodeIncomingEntityIDs
// remains a compatibility fallback for stores without materialized reachability.
//
// Reaching across repositories is the point of this read and also its hazard:
// the consumer row can belong to a repository the caller was not granted.
// allowedRepositoryIDs is that caller's grant, applied to the CONSUMER side
// (code_reachability_rows.repository_id) as a projected boolean rather than as
// a WHERE clause. Filtering the row out would leave the symbol looking
// unreferenced, which is a wrong answer, not a safe one; keeping it as a
// grant-less marker lets the caller be told the question cannot be decided from
// what they may read. An empty list is the unscoped caller, whose query omits the
// repository grant column and retains the two-column scan shape.
func (cr *ContentReader) CodeReachabilityIncomingEntityIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string]code.DeadCodeIncomingEdge, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]code.DeadCodeIncomingEdge{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "code_reachability_incoming_entity_ids"),
			attribute.String("db.sql.table", "code_reachability_rows"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	args := make([]any, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	grantColumn := ""
	candidateColumns := "entity_id, min_resolution_method, scope_id, generation_id"
	if len(allowedRepositoryIDs) > 0 {
		args = append(args, array.Of(allowedRepositoryIDs))
		candidateColumns += ", repository_id"
		grantColumn = fmt.Sprintf(", (candidate_rows.repository_id = ANY($%d)) AS consumer_in_grant", len(args))
	}
	// #nosec G202 -- concatenates only fixed column names and generated $N placeholders; entity IDs and grants are bound arguments, not SQL text
	query := `
		WITH candidate_rows AS MATERIALIZED (
			SELECT ` + candidateColumns + `
			FROM code_reachability_rows
			WHERE entity_id IN (` + strings.Join(placeholders, ", ") + `)
			  AND depth > 0
		)
		SELECT DISTINCT candidate_rows.entity_id, candidate_rows.min_resolution_method` + grantColumn + `
		FROM candidate_rows
		JOIN ingestion_scopes AS scope
		  ON scope.scope_id = candidate_rows.scope_id
		 AND scope.active_generation_id = candidate_rows.generation_id
		JOIN scope_generations AS generation
		  ON generation.generation_id = candidate_rows.generation_id
		 AND generation.status = 'active'
	`
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("code reachability incoming entity ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incoming := make(map[string]code.DeadCodeIncomingEdge)
	for rows.Next() {
		var entityID string
		var method sql.NullString
		inGrant := true
		if grantColumn == "" {
			if err := rows.Scan(&entityID, &method); err != nil {
				span.RecordError(err)
				return nil, fmt.Errorf("scan code reachability incoming entity id: %w", err)
			}
		} else if err := rows.Scan(&entityID, &method, &inGrant); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan code reachability incoming entity id: %w", err)
		}
		if !inGrant {
			deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, code.DeadCodeIncomingEdge{HiddenConsumer: true})
			continue
		}
		mergeDeadCodeIncomingEdge(incoming, entityID, method.String)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return incoming, nil
}

// DowngradedCodeRootKinds returns, per candidate entity, the set of guess-based
// dead-code root kinds the reducer's repo-wide #5376 verdict positively
// downgraded in the active generation. Only 'downgraded' rows are read; a
// confirmed verdict never appears here. A missing/lagging/non-active-generation
// verdict yields no row for that entity, so the caller keeps the parser root
// (lag-safety). Any error is returned; the caller fail-opens to KEEP.
func (cr *ContentReader) DowngradedCodeRootKinds(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]map[string]struct{}, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]map[string]struct{}{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "downgraded_code_root_kinds"),
			attribute.String("db.sql.table", "code_root_verdicts"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	// $1 = repoID, $2 = the downgraded verdict value bound from the shared
	// rubycontroller constant the reducer writes, so a rename of the verdict
	// value cannot silently desync this predicate (no bare 'downgraded' literal).
	args := make([]any, 0, len(entityIDs)+2)
	args = append(args, repoID, rubycontroller.VerdictDowngraded)
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from len(args)) into the IN list; entity ID values are bound args, not SQL text
	query := `
		SELECT verdict.entity_id, verdict.root_kind
		FROM code_root_verdicts AS verdict
		JOIN ingestion_scopes AS scope
		  ON scope.scope_id = verdict.scope_id
		 AND scope.active_generation_id = verdict.generation_id
		JOIN scope_generations AS generation
		  ON generation.generation_id = verdict.generation_id
		 AND generation.status = 'active'
		WHERE verdict.repository_id = $1
		  AND verdict.verdict = $2
		  AND verdict.entity_id IN (` + strings.Join(placeholders, ", ") + `)
	`
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("downgraded code root kinds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	downgraded := make(map[string]map[string]struct{})
	for rows.Next() {
		var entityID, rootKind string
		if err := rows.Scan(&entityID, &rootKind); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan downgraded code root kind: %w", err)
		}
		if downgraded[entityID] == nil {
			downgraded[entityID] = make(map[string]struct{})
		}
		downgraded[entityID][rootKind] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return downgraded, nil
}

// CodeReachabilityCoverage reports whether the active generation has a
// materialized reachability snapshot for repoID, and whether that snapshot hit
// the traversal bound. It is a status read only: since #7547 the dead-code
// incoming read no longer consults it, because a watermark does not prove the
// snapshot's roots were adequate, and the legacy incoming read runs for every
// entity the snapshot did not answer.
func (cr *ContentReader) CodeReachabilityCoverage(
	ctx context.Context,
	repoID string,
) (deadcode.CodeReachabilityCoverage, error) {
	repoID = strings.TrimSpace(repoID)
	if cr == nil || cr.db == nil || repoID == "" {
		return deadcode.CodeReachabilityCoverage{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "code_reachability_coverage"),
			attribute.String("db.sql.table", "code_reachability_repository_watermarks"),
		),
	)
	defer span.End()

	const query = `
		WITH active_watermarks AS (
			SELECT watermark.truncated
			FROM code_reachability_repository_watermarks AS watermark
			JOIN ingestion_scopes AS scope
			  ON scope.scope_id = watermark.scope_id
			 AND scope.active_generation_id = watermark.generation_id
			JOIN scope_generations AS generation
			  ON generation.generation_id = watermark.generation_id
			 AND generation.status = 'active'
			WHERE watermark.repository_id = $1
		)
		SELECT count(*) > 0 AS available,
		       coalesce(bool_or(truncated), false) AS truncated
		FROM active_watermarks
	`
	var coverage deadcode.CodeReachabilityCoverage
	if err := cr.db.QueryRowContext(ctx, query, repoID).Scan(&coverage.Available, &coverage.Truncated); err != nil {
		span.RecordError(err)
		return deadcode.CodeReachabilityCoverage{}, fmt.Errorf("code reachability coverage: %w", err)
	}
	return coverage, nil
}

// mergeDeadCodeIncomingEdge records the strongest incoming edge seen for
// entityID. Confidence is derived from method via codeprovenance.Confidence, so
// a missing or unrecorded method yields LegacyConfidence (strong) rather than a
// silent demotion.
// mergeDeadCodeIncomingEdge records one scanned edge for an entity. It routes
// through deadcode.MergeStrongestDeadCodeIncomingEdge so a granted edge arriving after an
// out-of-grant one keeps the hidden marker: the caller has both a consumer they
// can see and one they cannot, and only the second decides the answer.
func mergeDeadCodeIncomingEdge(incoming map[string]code.DeadCodeIncomingEdge, entityID, method string) {
	deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, code.DeadCodeIncomingEdge{
		MaxConfidence: codeprovenance.Confidence(method),
		Method:        method,
	})
}

func cleanDeadCodeIncomingEntityIDs(entityIDs []string) []string {
	if len(entityIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}

// crossRepoDeadCodeRowScanner is the Scan surface scanCrossRepoDeadCodeEvidence
// needs, so the cross-repo evidence decoder can be fed by *sql.Rows or a test row.
type crossRepoDeadCodeRowScanner interface {
	Scan(dest ...any) error
}

func scanCrossRepoDeadCodeEvidence(rows crossRepoDeadCodeRowScanner) (string, deadcode.CrossRepoDeadCodeEvidence, error) {
	var (
		entityID         string
		consumerRepoID   string
		consumerRepoName string
		rootEntityID     string
		depth            int
		state            string
		confidence       float64
		resolutionMethod string
		rawEvidence      []byte
		rawRootKinds     []byte
		generationID     string
		generationStatus string
		observedAt       time.Time
		updatedAt        time.Time
	)
	if err := rows.Scan(
		&entityID,
		&consumerRepoID,
		&consumerRepoName,
		&rootEntityID,
		&depth,
		&state,
		&confidence,
		&resolutionMethod,
		&rawEvidence,
		&rawRootKinds,
		&generationID,
		&generationStatus,
		&observedAt,
		&updatedAt,
	); err != nil {
		return "", deadcode.CrossRepoDeadCodeEvidence{}, fmt.Errorf("scan cross-repo dead code consumer evidence: %w", err)
	}
	var evidence []string
	if err := json.Unmarshal(rawEvidence, &evidence); err != nil {
		return "", deadcode.CrossRepoDeadCodeEvidence{}, fmt.Errorf("unmarshal cross-repo dead code evidence: %w", err)
	}
	var rootKinds []string
	if err := json.Unmarshal(rawRootKinds, &rootKinds); err != nil {
		return "", deadcode.CrossRepoDeadCodeEvidence{}, fmt.Errorf("unmarshal cross-repo dead code root kinds: %w", err)
	}
	item := deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepoID,
		ConsumerRepoName: consumerRepoName,
		ConsumerEntityID: rootEntityID,
		RelationshipType: crossRepoDeadCodeRelationshipType(evidence),
		EvidenceFamily:   "direct_code",
		Citation:         crossRepoDeadCodeCitation(generationID, consumerRepoID, rootEntityID, entityID),
		Confidence:       confidence,
		ConfidenceLabel:  deadcode.CrossRepoDeadCodeConfidenceLabel(confidence),
		ResolutionMethod: resolutionMethod,
		Depth:            depth,
		GenerationID:     generationID,
		GenerationStatus: generationStatus,
		ObservedAt:       observedAt,
		Ambiguous:        strings.EqualFold(state, "ambiguous"),
	}
	if !strings.EqualFold(generationStatus, "active") {
		item.NeedsEvidence = true
		item.Reason = "stale_generation"
	}
	if item.Ambiguous {
		item.NeedsEvidence = true
		item.Reason = "ambiguous_consumer_ownership"
	}
	return entityID, item, nil
}

func crossRepoDeadCodeRelationshipType(evidence []string) string {
	for _, value := range evidence {
		for _, relationship := range []string{"CALLS", "REFERENCES", "INHERITS", "IMPORTS"} {
			if strings.Contains(strings.ToUpper(value), relationship) {
				return relationship
			}
		}
	}
	return "REACHES"
}

func crossRepoDeadCodeCitation(generationID string, consumerRepoID string, rootEntityID string, entityID string) string {
	return "code_reachability_rows:" + generationID + "/" + consumerRepoID + "/" + rootEntityID + "/" + entityID
}

// CrossRepoDeadCodeConsumerCoverage reports which consumer repositories the
// request covers lack a complete reachability snapshot for their active
// generation. The dead-code route calls it once per request: a producer symbol
// with no consumer row is only dead when this answer is complete, because a
// repository whose snapshot is missing, truncated, or built under an older
// verdict schema epoch (reachabilitystore.CodeReachabilityVerdictSchemaEpoch,
// bound by this method) contributes no rows, or wrong ones, for symbols it really
// does call (#7547). Those are the only three gaps it detects: a
// stale or partly drained snapshot whose watermark says truncated = false reads
// complete, as does a zero-root repository until the writer stamps it truncated.
//
// Each gap carries why it is one (no_snapshot_yet, truncated, older_epoch or
// no_active_scope), the scope's active generation id, and whether waiting can fix
// it. The named statement returns one row per repository; the
// all-repositories statement can return one per scope, and this method keeps one
// per repository by the rule on code.CrossRepoDeadCodeCoverageGap. A state this
// method does not know is an error, never a made-up retry promise.
//
// An empty request that is not AllRepositories is refused rather than answered
// "complete": naming no repository proves nothing about any of them.
func (cr *ContentReader) CrossRepoDeadCodeConsumerCoverage(
	ctx context.Context,
	request code.CrossRepoDeadCodeCoverageRequest,
) (code.CrossRepoDeadCodeCoverage, error) {
	if cr == nil || cr.db == nil {
		return code.CrossRepoDeadCodeCoverage{}, fmt.Errorf("cross-repo dead code consumer coverage: content reader unavailable")
	}
	if !request.AllRepositories && len(request.RepositoryIDs) == 0 {
		return code.CrossRepoDeadCodeCoverage{}, fmt.Errorf("cross-repo dead code consumer coverage: request names no repositories")
	}

	ctx, span := cr.tracer.Start(
		ctx,
		"postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "cross_repo_dead_code_consumer_coverage"),
			attribute.String("db.sql.table", "code_reachability_repository_watermarks"),
			attribute.Bool("db.coverage.all_repositories", request.AllRepositories),
			attribute.Int("db.coverage.requested_repositories", len(request.RepositoryIDs)),
		),
	)
	defer span.End()

	limit := deadcode.CrossRepoDeadCodeCoverageGapCap + 1
	epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch
	query, args := deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery, []any{epoch, limit}
	if !request.AllRepositories {
		query = deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery
		args = []any{array.Of(request.RepositoryIDs), request.RequireActiveScope, epoch, limit}
	}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return code.CrossRepoDeadCodeCoverage{}, fmt.Errorf("cross-repo dead code consumer coverage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// A repository behind several scopes can come back once per scope on the
	// all-repositories read; keep one gap per repository.
	picked := make(map[string]code.CrossRepoDeadCodeCoverageGap, limit)
	rowCount := 0
	for rows.Next() {
		rowCount++
		var repositoryID string
		var state, generationID sql.NullString
		if err := rows.Scan(&repositoryID, &state, &generationID); err != nil {
			span.RecordError(err)
			return code.CrossRepoDeadCodeCoverage{}, fmt.Errorf("scan cross-repo dead code consumer coverage: %w", err)
		}
		gap := code.CrossRepoDeadCodeCoverageGap{
			RepositoryID: repositoryID,
			State:        state.String,
			GenerationID: generationID.String,
			Retryable:    code.CrossRepoDeadCodeCoverageGapRetryable(state.String),
		}
		if !state.Valid || !gap.KnownState() {
			err := fmt.Errorf("cross-repo dead code consumer coverage: repository %q has unknown gap state %q", repositoryID, state.String)
			span.RecordError(err)
			return code.CrossRepoDeadCodeCoverage{}, err
		}
		if current, ok := picked[repositoryID]; !ok || gap.Outranks(current) {
			picked[repositoryID] = gap
		}
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return code.CrossRepoDeadCodeCoverage{}, fmt.Errorf("cross-repo dead code consumer coverage: %w", err)
	}

	gaps := make([]code.CrossRepoDeadCodeCoverageGap, 0, len(picked))
	for _, gap := range picked {
		gaps = append(gaps, gap)
	}
	slices.SortFunc(gaps, func(a, b code.CrossRepoDeadCodeCoverageGap) int {
		return strings.Compare(a.RepositoryID, b.RepositoryID)
	})
	coverage := code.CrossRepoDeadCodeCoverage{}
	// Reaching the limit means the statement may have stopped short, which a
	// repository reported by two scopes can hide behind the dedupe above.
	if rowCount >= limit {
		coverage.IncompleteTruncated = true
	}
	if len(gaps) > deadcode.CrossRepoDeadCodeCoverageGapCap {
		gaps = gaps[:deadcode.CrossRepoDeadCodeCoverageGapCap]
	}
	if len(gaps) > 0 {
		coverage.Gaps = gaps
	}
	span.SetAttributes(
		attribute.Int("db.rows.incomplete_consumer_repositories", len(gaps)),
		attribute.Bool("db.coverage.truncated", coverage.IncompleteTruncated),
		attribute.Bool("db.coverage.retryable", coverage.Retryable()),
	)
	return coverage, nil
}
