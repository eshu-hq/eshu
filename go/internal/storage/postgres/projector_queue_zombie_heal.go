// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Zombie-heal outcomes label eshu_dp_projector_zombie_heal_total. The set is
// closed: the refusal paths below emit exactly one of these per refused
// zombie attempt, and the live tests pin each value.
const (
	// zombieHealOutcomeHealed re-opened the active generation row.
	zombieHealOutcomeHealed = "healed"
	// zombieHealOutcomeSkippedInFlight left a live active claim alone.
	zombieHealOutcomeSkippedInFlight = "skipped_in_flight"
	// zombieHealOutcomeSkippedAlreadyOpen found the active row already open.
	zombieHealOutcomeSkippedAlreadyOpen = "skipped_already_open"
	// zombieHealOutcomeSkippedNoMarker refused a generation that never wrote.
	zombieHealOutcomeSkippedNoMarker = "skipped_no_write_marker"
	// zombieHealOutcomeSkippedNoActive found no other active generation.
	zombieHealOutcomeSkippedNoActive = "skipped_no_active_generation"
	// zombieHealOutcomeError counts a heal statement failure.
	zombieHealOutcomeError = "error"
)

// healProjectorActiveGenerationQuery re-opens the scope's current active
// generation projector row after a zombie refusal (#7209). A zombie that
// wrote for its generation and was refused after a successor published may
// have retracted the successor's canonical nodes; re-driving the active
// generation re-projects over that damage. The upsert mirrors the liveness
// re-enqueue in generation_liveness_sql.go: same canonical work_item_id, same
// reopened columns, same write-time in-flight guard, ON CONFLICT DO UPDATE
// so repeated refusals converge on one open row.
//
// Gates, all evaluated in one statement snapshot (arbiter Option B):
//
//   - the refused generation ($3) has projection_write_started_at IS NOT
//     NULL: it marked before its first write, so it may have written. A
//     retired generation can never mark afterwards, and supersede paths
//     never clear the marker, so NULL means it provably never wrote.
//   - the scope has a current active generation that differs from the
//     refused one and is still active. Healing the refused generation
//     itself would revive a superseded generation.
//   - the active row is not in flight at write time: the ON CONFLICT WHERE
//     rechecks pending/retrying/live-claimed on the locked conflict row
//     (EvalPlanQual), so a concurrent claim or a prior heal wins and this
//     heal skips instead of clobbering it.
//
// The statement locks no scope or generation row. A blocking scope lock
// would stall the refusing Heartbeat or Ack behind ingestion commits that
// hold the scope row while streaming facts, and SKIP LOCKED would silently
// drop a heal the dead zombie never retries. The residual is a TOCTOU miss:
// an Ack that commits mid-statement can move the pointer after this
// statement's snapshot, reopening a just-superseded generation (the claim
// sweep re-retires that row) or missing a just-published one. Both are
// today's behavior plus a swept row, strictly better than never healing.
//
// The outcome label is computed from the same snapshot, except the
// already-open versus in-flight split can race a concurrent claim: the
// upsert's write-time guard stays exact, only the label may name the
// sibling skip. Telemetry only.
//
// Parameter order:
//
//	$1 now              (work item visibility/update stamp, lease comparison)
//	$2 scope_id         (refused work's scope)
//	$3 refused generation (the zombie's superseded generation)
//
// It returns one row: the healed generation_id (NULL unless healed) and the
// outcome label.
const healProjectorActiveGenerationQuery = `
WITH refused AS (
    SELECT generation.projection_write_started_at IS NOT NULL AS wrote
    FROM scope_generations AS generation
    WHERE generation.scope_id = $2
      AND generation.generation_id = $3
),
target AS (
    SELECT scope.active_generation_id AS generation_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS active
      ON active.scope_id = scope.scope_id
     AND active.generation_id = scope.active_generation_id
    JOIN refused ON refused.wrote
    WHERE scope.scope_id = $2
      AND scope.active_generation_id IS NOT NULL
      AND scope.active_generation_id <> $3
      AND active.status = 'active'
),
active_row AS (
    SELECT work.status, work.claim_until
    FROM target
    JOIN fact_work_items AS work
      ON work.work_item_id = 'projector_' || $2 || '_' || target.generation_id
),
upserted AS (
    INSERT INTO fact_work_items (
        work_item_id,
        scope_id,
        generation_id,
        stage,
        domain,
        status,
        attempt_count,
        lease_owner,
        claim_until,
        visible_at,
        last_attempt_at,
        next_attempt_at,
        failure_class,
        failure_message,
        failure_details,
        payload,
        created_at,
        updated_at
    )
    SELECT
        'projector_' || $2 || '_' || target.generation_id,
        $2,
        target.generation_id,
        'projector',
        'source_local',
        'pending',
        0,
        NULL,
        NULL,
        $1,
        NULL,
        NULL,
        NULL,
        NULL,
        NULL,
        jsonb_build_object('zombie_heal_count', 1),
        $1,
        $1
    FROM target
    ON CONFLICT (work_item_id) DO UPDATE
    SET status = 'pending',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = EXCLUDED.visible_at,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        payload = jsonb_set(
            COALESCE(fact_work_items.payload, '{}'::jsonb),
            '{zombie_heal_count}',
            to_jsonb(COALESCE((fact_work_items.payload ->> 'zombie_heal_count')::int, 0) + 1)
        ),
        updated_at = EXCLUDED.updated_at
    WHERE NOT (
        fact_work_items.status IN ('pending', 'retrying')
        OR (
            fact_work_items.status IN ('claimed', 'running')
            AND fact_work_items.claim_until > $1
        )
    )
    RETURNING generation_id
)
SELECT
    (SELECT generation_id FROM upserted) AS healed_generation_id,
    CASE
        WHEN EXISTS (SELECT 1 FROM upserted) THEN '` + zombieHealOutcomeHealed + `'
        WHEN EXISTS (SELECT 1 FROM target) THEN
            CASE WHEN EXISTS (
                SELECT 1 FROM active_row
                WHERE status IN ('claimed', 'running') AND claim_until > $1
            ) THEN '` + zombieHealOutcomeSkippedInFlight + `'
            ELSE '` + zombieHealOutcomeSkippedAlreadyOpen + `'
            END
        WHEN EXISTS (SELECT 1 FROM refused WHERE wrote) THEN '` + zombieHealOutcomeSkippedNoActive + `'
        ELSE '` + zombieHealOutcomeSkippedNoMarker + `'
    END AS outcome
`

// healZombieActiveGenerationResult is one heal statement's outcome.
type healZombieActiveGenerationResult struct {
	healedGenerationID string
	outcome            string
}

// healZombieActiveGeneration runs the heal statement for one refused zombie
// attempt and returns which generation was re-opened, if any, with the
// outcome label. A statement error returns outcome "error" so the caller
// still counts the attempt.
func (q ProjectorQueue) healZombieActiveGeneration(
	ctx context.Context,
	scopeID string,
	refusedGenerationID string,
	now time.Time,
) healZombieActiveGenerationResult {
	rows, err := q.database.QueryContext(ctx, healProjectorActiveGenerationQuery,
		now, scopeID, refusedGenerationID)
	if err != nil {
		return healZombieActiveGenerationResult{outcome: zombieHealOutcomeError}
	}
	defer func() { _ = rows.Close() }()
	var healed sql.NullString
	var outcome string
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return healZombieActiveGenerationResult{outcome: zombieHealOutcomeError}
		}
		return healZombieActiveGenerationResult{outcome: zombieHealOutcomeError}
	}
	if err := rows.Scan(&healed, &outcome); err != nil {
		return healZombieActiveGenerationResult{outcome: zombieHealOutcomeError}
	}
	if err := rows.Err(); err != nil {
		return healZombieActiveGenerationResult{outcome: zombieHealOutcomeError}
	}
	return healZombieActiveGenerationResult{healedGenerationID: healed.String, outcome: outcome}
}

// healZombieRefusal heals the scope's active generation after one zombie
// refusal and records the outcome. It is best-effort: the refusal already
// stopped the zombie, which is the primary contract, so a heal failure is
// logged and counted, never returned.
func (q ProjectorQueue) healZombieRefusal(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	now time.Time,
) {
	result := q.healZombieActiveGeneration(ctx, work.Scope.ScopeID, work.Generation.GenerationID, now)
	recordZombieHeal(ctx, q.Instruments, result.outcome)
	switch result.outcome {
	case zombieHealOutcomeHealed:
		slog.InfoContext(ctx, "projector zombie refusal healed the active generation",
			slog.String(telemetry.LogKeyScopeID, work.Scope.ScopeID),
			slog.String(telemetry.LogKeyRefusedGenerationID, work.Generation.GenerationID),
			slog.String(telemetry.LogKeyHealedGenerationID, result.healedGenerationID),
			slog.String(telemetry.LogKeyOutcome, result.outcome),
		)
	case zombieHealOutcomeError:
		slog.ErrorContext(ctx, "projector zombie heal failed; the active generation may be missing nodes",
			slog.String(telemetry.LogKeyScopeID, work.Scope.ScopeID),
			slog.String(telemetry.LogKeyRefusedGenerationID, work.Generation.GenerationID),
			slog.String(telemetry.LogKeyOutcome, result.outcome),
		)
	default:
		slog.DebugContext(ctx, "projector zombie refusal skipped the active-generation heal",
			slog.String(telemetry.LogKeyScopeID, work.Scope.ScopeID),
			slog.String(telemetry.LogKeyRefusedGenerationID, work.Generation.GenerationID),
			slog.String(telemetry.LogKeyOutcome, result.outcome),
		)
	}
}

// recordZombieHeal counts one zombie-heal outcome on
// eshu_dp_projector_zombie_heal_total. Nil instruments are a no-op.
func recordZombieHeal(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	if instruments == nil || instruments.ProjectorZombieHeal == nil {
		return
	}
	instruments.ProjectorZombieHeal.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}
