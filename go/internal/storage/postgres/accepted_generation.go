// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RelationshipGenerationActiveChecker reports whether a relationship generation
// is active (published) in Postgres. RelationshipStore satisfies it.
type RelationshipGenerationActiveChecker interface {
	IsGenerationActive(ctx context.Context, generationID string) (bool, error)
}

// NewRelationshipGenerationActiveLookup adapts a generation active-status
// checker to the reducer authority gate. It backs the repo-dependency lane so an
// accepted generation only grants graph-projection authority once the
// relationship generation is activated, keeping the graph from running ahead of
// the Postgres relationship read models.
//
// The lookup type is synchronous by design — it is invoked from per-row
// authority checks deep inside graph-runner loops where no request context
// is in scope — so the adapter uses context.Background() for the single
// indexed status read. Cancellation-safety for request-scoped passes lives
// one layer up: the corpus-wide fence lookup
// (NewRelationshipGenerationsCompleteLookup) takes the caller's context
// (#6730).
func NewRelationshipGenerationActiveLookup(
	checker RelationshipGenerationActiveChecker,
) accepted.RelationshipGenerationActiveLookup {
	return func(generationID string) (bool, error) {
		generationID = strings.TrimSpace(generationID)
		if generationID == "" {
			return false, nil
		}
		return checker.IsGenerationActive(context.Background(), generationID)
	}
}

// errActiveScopeRelationshipGenerationsNoRows is the concrete no-cause error
// for the defensive empty-row branch below: the NOT EXISTS aggregate always
// yields one row, so reaching it means the backend violated the query shape.
// It is deliberately not a %w wrap — there is no underlying error to chain,
// and wrapping a nil rows.Err() formats as %!w(<nil>).
var errActiveScopeRelationshipGenerationsNoRows = errors.New(
	"active scope relationship generations complete: no rows returned",
)

// RelationshipGenerationsCompleteChecker reports whether every active scope's
// current relationship generation is active. RelationshipStore satisfies it.
type RelationshipGenerationsCompleteChecker interface {
	AreActiveScopeRelationshipGenerationsComplete(ctx context.Context) (bool, error)
}

// NewRelationshipGenerationsCompleteLookup adapts a corpus-wide completeness
// checker to the workload and deployable-unit correlation input gates. A
// lookup error fails safe as incomplete: derivation must defer rather than
// succeed on a possibly partial foreign resolved set.
func NewRelationshipGenerationsCompleteLookup(
	checker RelationshipGenerationsCompleteChecker,
) accepted.RelationshipGenerationsCompleteLookup {
	return func(ctx context.Context) (bool, error) {
		return checker.AreActiveScopeRelationshipGenerationsComplete(ctx)
	}
}

// RelationshipGenerationsIncompleteScopesChecker lists the active scope IDs
// holding the corpus fence. RelationshipStore satisfies it.
type RelationshipGenerationsIncompleteScopesChecker interface {
	IncompleteActiveScopeRelationshipGenerations(ctx context.Context) ([]string, error)
}

// NewRelationshipGenerationsIncompleteScopesLookup adapts the holder-listing
// checker to the workload and deployable-unit correlation deferral errors.
// Best-effort by contract: the gate ignores a lookup error and omits the
// holder list rather than blocking the deferral on diagnosability.
func NewRelationshipGenerationsIncompleteScopesLookup(
	checker RelationshipGenerationsIncompleteScopesChecker,
) accepted.RelationshipGenerationsIncompleteScopesLookup {
	return func(ctx context.Context) ([]string, error) {
		return checker.IncompleteActiveScopeRelationshipGenerations(ctx)
	}
}

// IncompleteActiveScopeRelationshipGenerations lists the active scope IDs
// whose current relationship generation is not complete, ordered for stable
// deferral messages. It shares its predicate with the boolean completeness
// query so the holder list can never disagree with the gate (#6730).
func (s *RelationshipStore) IncompleteActiveScopeRelationshipGenerations(
	ctx context.Context,
) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, incompleteActiveScopeRelationshipGenerationsSQL)
	if err != nil {
		return nil, fmt.Errorf("query incomplete scope relationship generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopeIDs []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan incomplete scope relationship generations: %w", err)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incomplete scope relationship generations: %w", err)
	}
	return scopeIDs, nil
}

// resolutionProducerStateSQL reads the scope's active generation, the
// generation's relationship_generations status, and the newest
// deployment_mapping work item of one scope generation (#7597). It runs only
// when an own-gate consumer already defers, and every read is an index scan:
// ingestion_scopes_pkey, relationship_generations_pkey, and
// fact_work_items_scope_generation_idx (scope_id, generation_id, ...),
// filtering the generation's few reducer rows on stage and domain. Evidence:
// docs/internal/evidence/7597-own-gate-producer-terminal.md.
const resolutionProducerStateSQL = `
SELECT COALESCE(s.active_generation_id, ''),
       COALESCE(w.work_item_id, ''), COALESCE(w.status, ''), COALESCE(w.failure_class, ''),
       COALESCE(rg.status, '')
FROM ingestion_scopes AS s
LEFT JOIN relationship_generations AS rg
  ON rg.generation_id = $2
 AND rg.scope = s.scope_id
LEFT JOIN LATERAL (
    SELECT d.work_item_id, d.status, d.failure_class
    FROM fact_work_items AS d
    WHERE d.scope_id = s.scope_id
      AND d.generation_id = $2
      AND d.stage = 'reducer'
      AND d.domain = 'deployment_mapping'
    ORDER BY d.updated_at DESC, d.work_item_id DESC
    LIMIT 1
) AS w ON TRUE
WHERE s.scope_id = $1
`

// NewResolutionProducerLookup reads the own-gate producer state the
// deployable-unit and workload consumers consult when their own relationship
// generation is inactive (crossrepo.EvaluateOwnGate). A missing scope row
// reads as an empty state, which the gate treats as "scope moved on" and
// defers. A query error is returned for the gate to classify.
func NewResolutionProducerLookup(database db.Queryer) crossrepo.ProducerLookup {
	return func(ctx context.Context, scopeID, generationID string) (crossrepo.ProducerState, error) {
		rows, err := database.QueryContext(ctx, resolutionProducerStateSQL, scopeID, generationID)
		if err != nil {
			return crossrepo.ProducerState{}, fmt.Errorf("query resolution producer state: %w", err)
		}
		defer func() { _ = rows.Close() }()
		var state crossrepo.ProducerState
		if rows.Next() {
			if err := rows.Scan(
				&state.ScopeActiveGenerationID, &state.WorkItemID, &state.Status, &state.FailureClass,
				&state.RelationshipGenerationStatus,
			); err != nil {
				return crossrepo.ProducerState{}, fmt.Errorf("scan resolution producer state: %w", err)
			}
		}
		if err := rows.Err(); err != nil {
			return crossrepo.ProducerState{}, fmt.Errorf("iterate resolution producer state: %w", err)
		}
		return state, nil
	}
}

// NewAcceptedGenerationLookup creates an exact bounded-unit acceptance lookup
// backed by shared_projection_acceptance.
//
// Like NewRelationshipGenerationActiveLookup above, the lookup type is
// synchronous by design — per-row acceptance checks run where no request
// context is in scope — so the adapter uses context.Background() for the
// single indexed key read. A lookup error fails safe as not-found, and
// request-scoped cancellation is enforced by the fence and queue layers
// above (#6730).
func NewAcceptedGenerationLookup(database db.ExecQueryer) reducer.AcceptedGenerationLookup {
	store := NewSharedProjectionAcceptanceStore(database)
	return func(key reducer.SharedProjectionAcceptanceKey) (string, bool) {
		generationID, found, err := store.Lookup(
			context.Background(),
			key.ScopeID,
			key.AcceptanceUnitID,
			key.SourceRunID,
		)
		if err != nil {
			return "", false
		}
		return generationID, found
	}
}

// NewAcceptedGenerationPrefetch batches acceptance lookups for a current
// partition slice and returns an in-memory lookup closure for the reducer hot
// path. This keeps the shared runner collector-agnostic while avoiding repeated
// store calls for duplicate bounded-unit keys.
//
// The batch resolves through SharedProjectionAcceptanceStore.LookupBatch:
// one UNNEST/JOIN query per 1000 distinct keys (#7724 1A), replacing the
// former one-Lookup-per-key loop. Per-key found/not-found semantics,
// TrimSpace normalization, and the batch-error-fails-selection contract
// are unchanged.
func NewAcceptedGenerationPrefetch(database db.ExecQueryer) reducer.AcceptedGenerationPrefetch {
	store := NewSharedProjectionAcceptanceStore(database)

	return func(ctx context.Context, intents []sharedintent.Row) (reducer.AcceptedGenerationLookup, error) {
		keys := make([]sharedintent.AcceptanceKey, 0, len(intents))
		for _, intent := range intents {
			key, ok := intent.AcceptanceKey()
			if !ok {
				continue
			}
			keys = append(keys, key)
		}

		acceptedByKey, err := store.LookupBatch(ctx, keys)
		if err != nil {
			return nil, err
		}

		return func(key reducer.SharedProjectionAcceptanceKey) (string, bool) {
			key.ScopeID = strings.TrimSpace(key.ScopeID)
			key.AcceptanceUnitID = strings.TrimSpace(key.AcceptanceUnitID)
			key.SourceRunID = strings.TrimSpace(key.SourceRunID)
			generationID, ok := acceptedByKey[key]
			return generationID, ok
		}, nil
	}
}

// AreActiveScopeRelationshipGenerationsComplete reports whether derivation
// that merges foreign resolved reads may proceed: every active scope either
// has its current relationship generation active, or has no generation row
// and no live resolution work that could still produce one. It backs the
// workload and deployable-unit correlation input gates so derivation defers
// until the corpus-wide resolved set is complete (#6184) without waiting on
// scopes that will never resolve (#6730). A single boolean row always
// returns; a missing row is impossible from the NOT EXISTS shape, and a
// query error fails safe as incomplete.
func (s *RelationshipStore) AreActiveScopeRelationshipGenerationsComplete(
	ctx context.Context,
) (bool, error) {
	rows, err := s.database.QueryContext(ctx, activeScopeRelationshipGenerationsCompleteSQL)
	if err != nil {
		return false, fmt.Errorf("query active scope relationship generations complete: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("active scope relationship generations complete: iterate rows: %w", err)
		}
		return false, errActiveScopeRelationshipGenerationsNoRows
	}
	var complete bool
	if err := rows.Scan(&complete); err != nil {
		return false, fmt.Errorf("scan active scope relationship generations complete: %w", err)
	}
	return complete, rows.Err()
}

// sharedProjectionAcceptanceLookupChunkSize bounds one batched
// acceptance lookup (#7724 1A). Each chunk is a single UNNEST/JOIN
// query over at most this many distinct keys, so a widen round costs
// ceil(keys/1000) queries instead of one Lookup per key.
const sharedProjectionAcceptanceLookupChunkSize = 1000

// lookupSharedProjectionAcceptanceBatchSQL resolves one chunk of exact
// bounded-unit keys in a single round trip (#7724 1A). The multi-argument
// UNNEST zips the three key arrays positionally and the JOIN probes the
// (scope_id, acceptance_unit_id, source_run_id) primary key once per key,
// so the plan is a nested loop over PK index scans with no sequential
// scan. Only present keys return rows; the caller treats absent keys as
// not-found, exactly like a Lookup miss.
const lookupSharedProjectionAcceptanceBatchSQL = `
SELECT a.scope_id, a.acceptance_unit_id, a.source_run_id, a.generation_id
FROM UNNEST($1::text[], $2::text[], $3::text[]) AS k(scope_id, acceptance_unit_id, source_run_id)
JOIN shared_projection_acceptance AS a
  ON a.scope_id = k.scope_id
 AND a.acceptance_unit_id = k.acceptance_unit_id
 AND a.source_run_id = k.source_run_id
`

// LookupBatch returns the accepted generation for each exact bounded-unit
// key present in shared_projection_acceptance (#7724 1A). It issues one
// UNNEST/JOIN query per chunk of at most
// sharedProjectionAcceptanceLookupChunkSize distinct keys, so a widen
// round costs ceil(keys/1000) queries instead of one Lookup per key.
//
// Keys are normalized (TrimSpace) before the query and compared on
// normalized keys, matching the prefetch closure's comparison; blank keys
// cannot match and are skipped without a query. Absent keys are simply
// missing from the result, exactly like a Lookup miss. A query error fails
// the whole batch: the caller must fail its selection rather than drop or
// keep rows. An empty key set issues no query.
//
// The call records keys/queries/rows/duration into the context's
// PrefetchStats when the caller attached one.
func (s *SharedProjectionAcceptanceStore) LookupBatch(
	ctx context.Context,
	keys []sharedintent.AcceptanceKey,
) (map[sharedintent.AcceptanceKey]string, error) {
	seen := make(map[sharedintent.AcceptanceKey]struct{}, len(keys))
	distinct := make([]sharedintent.AcceptanceKey, 0, len(keys))
	for _, key := range keys {
		key.ScopeID = strings.TrimSpace(key.ScopeID)
		key.AcceptanceUnitID = strings.TrimSpace(key.AcceptanceUnitID)
		key.SourceRunID = strings.TrimSpace(key.SourceRunID)
		if key.ScopeID == "" || key.AcceptanceUnitID == "" || key.SourceRunID == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		distinct = append(distinct, key)
	}
	if len(distinct) == 0 {
		return map[sharedintent.AcceptanceKey]string{}, nil
	}

	start := time.Now()
	acceptedByKey := make(map[sharedintent.AcceptanceKey]string, len(distinct))
	queries := 0
	for i := 0; i < len(distinct); i += sharedProjectionAcceptanceLookupChunkSize {
		end := i + sharedProjectionAcceptanceLookupChunkSize
		if end > len(distinct) {
			end = len(distinct)
		}
		if err := s.lookupAcceptanceChunk(ctx, distinct[i:end], acceptedByKey); err != nil {
			sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindAcceptance, len(distinct), queries, len(acceptedByKey), 0, time.Since(start))
			return nil, err
		}
		queries++
	}
	sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindAcceptance, len(distinct), queries, len(acceptedByKey), 0, time.Since(start))
	return acceptedByKey, nil
}

// lookupAcceptanceChunk resolves one chunk of normalized distinct keys
// with a single UNNEST/JOIN query and merges the found rows into out.
func (s *SharedProjectionAcceptanceStore) lookupAcceptanceChunk(
	ctx context.Context,
	chunk []sharedintent.AcceptanceKey,
	out map[sharedintent.AcceptanceKey]string,
) error {
	scopes := make([]string, len(chunk))
	units := make([]string, len(chunk))
	runs := make([]string, len(chunk))
	for i, key := range chunk {
		scopes[i] = key.ScopeID
		units[i] = key.AcceptanceUnitID
		runs[i] = key.SourceRunID
	}
	rows, err := s.database.QueryContext(ctx, lookupSharedProjectionAcceptanceBatchSQL, scopes, units, runs)
	if err != nil {
		return fmt.Errorf("query shared projection acceptance batch (%d keys): %w", len(chunk), err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scopeID, acceptanceUnitID, sourceRunID, generationID string
		if err := rows.Scan(&scopeID, &acceptanceUnitID, &sourceRunID, &generationID); err != nil {
			return fmt.Errorf("scan shared projection acceptance batch: %w", err)
		}
		out[sharedintent.AcceptanceKey{
			ScopeID:          strings.TrimSpace(scopeID),
			AcceptanceUnitID: strings.TrimSpace(acceptanceUnitID),
			SourceRunID:      strings.TrimSpace(sourceRunID),
		}] = generationID
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate shared projection acceptance batch (%d keys): %w", len(chunk), err)
	}
	return nil
}

// ownGateCountingClasses is every own-gate counting class: the consumers
// dead-lettered because their producer was dead-lettered or missing. An
// activation satisfies the wait of both, so the wake reopens both.
func ownGateCountingClasses() []string {
	return append(crossrepo.ProducerDeadLetteredFailureClasses(), crossrepo.ProducerMissingFailureClasses()...)
}

// ownGateConsumerDomainStrings renders crossrepo.OwnGateConsumerDomains.
func ownGateConsumerDomainStrings() []string {
	domains := crossrepo.OwnGateConsumerDomains()
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		out = append(out, string(domain))
	}
	return out
}

// sqlStringList renders compile-time constant names as a SQL literal list. It
// is only for constants owned by this repository, never input.
func sqlStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+strings.ReplaceAll(value, "'", "''")+"'")
	}
	return strings.Join(quoted, ", ")
}

// wakeAfterProducerTerminalDeadLetter closes the one interleave the
// activation wake alone cannot: a consumer whose final counted Fail waited on
// an activation's in-flight lock, then dead-lettered after that activation
// committed (#7597). This fresh-snapshot statement sees the active generation
// and reopens the row at once; it is a no-op while the generation is
// inactive. It runs only for an own-gate counting dead letter. The dead letter
// already committed, so a wake failure is logged at ERROR, not returned: the
// row stays dead-lettered and a later activation or replay reopens it.
func (q ReducerQueue) wakeAfterProducerTerminalDeadLetter(
	ctx context.Context,
	intent reducercontract.Intent,
	failureClass string,
	now time.Time,
) {
	if !slices.Contains(ownGateCountingClasses(), failureClass) {
		return
	}
	result, err := q.database.ExecContext(ctx, wakeProducerTerminalConsumersSQL, now,
		intent.ScopeID, intent.GenerationID)
	if err != nil {
		recordOwnGateWake(ctx, q.Instruments, telemetry.OwnGateWakeSiteFail, telemetry.OwnGateWakeOutcomeError)
		slog.Default().ErrorContext(ctx, "own-gate dead letter could not check for an active generation",
			"work_item_id", intent.IntentID, "scope_id", intent.ScopeID,
			"generation_id", intent.GenerationID, "failure_class", failureClass, "error", err)
		return
	}
	n, _ := result.RowsAffected()
	recordOwnGateWake(ctx, q.Instruments, telemetry.OwnGateWakeSiteFail, wakeOutcome(n))
	if n > 0 {
		slog.Default().InfoContext(ctx, "own-gate dead letter reopened: relationship generation already active",
			"work_item_id", intent.IntentID, "scope_id", intent.ScopeID,
			"generation_id", intent.GenerationID, "failure_class", failureClass, "woken_consumers", n)
	}
}

// wakeOutcome maps a wake statement's affected rows to its outcome label.
func wakeOutcome(reopened int64) string {
	if reopened > 0 {
		return telemetry.OwnGateWakeOutcomeReopened
	}
	return telemetry.OwnGateWakeOutcomeNoop
}

// recordOwnGateWake counts one wake statement on
// eshu_dp_reducer_own_gate_wake_total (#7597). Nil instruments are a no-op.
func recordOwnGateWake(ctx context.Context, instruments *telemetry.Instruments, site, outcome string) {
	if instruments == nil || instruments.ReducerOwnGateWake == nil {
		return
	}
	instruments.ReducerOwnGateWake.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrOwnGateWakeSite(site), telemetry.AttrOutcome(outcome),
	))
}
