// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ClaimBatch claims up to limit reducer work items in a single Postgres
// round-trip using FOR UPDATE SKIP LOCKED. Implements reducer.BatchWorkSource.
func (q ReducerQueue) ClaimBatch(ctx context.Context, limit int) ([]reducer.Intent, error) {
	if err := q.validateClaim(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 16
	}

	now := q.now()
	rows, err := q.db.QueryContext(
		ctx,
		claimReducerWorkBatchQuery,
		now,
		q.claimDomainFilters(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
		limit,
	)
	if err != nil {
		if isReducerLiveLeaseConflict(err) {
			// The batch claim restricts candidates to one representative row per
			// conflict key, so it does not race itself; this only fires if a
			// concurrent single-claim worker took a sibling on the same key
			// first. The whole batch statement rolled back — return empty and let
			// the next poll re-claim (#4137).
			return nil, nil
		}
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var intents []reducer.Intent
	for rows.Next() {
		intent, err := scanReducerIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("batch claim scan: %w", err)
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		if isReducerLiveLeaseConflict(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}

	return intents, nil
}

// AckBatch acknowledges multiple claimed reducer work items. Target-domain
// rows use exact claim epochs; mixed-domain batches use one exact target update
// and one legacy update so unrelated success cannot mask a fully stale target
// subset. Implements reducer.BatchWorkSink.
func (q ReducerQueue) AckBatch(ctx context.Context, intents []reducer.Intent, _ []reducer.Result) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()

	targetIntents, cicdIntents, unrelatedIntents, err := splitReducerAckBatchIntents(intents)
	if err != nil {
		return err
	}

	targetClaimRejected := false
	if len(targetIntents) > 0 {
		query, args := ackContainerImageIdentityReducerWorkBatchQuery(
			now,
			q.LeaseOwner,
			targetIntents,
		)
		result, err := q.db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf(
				"batch ack reducer work (%d target items): %w",
				len(targetIntents),
				err,
			)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("batch ack reducer work: rows affected: %w", err)
		}
		targetClaimRejected = rowsAffected == 0
	}
	if len(cicdIntents) > 0 {
		query, args := ackCICDRunCorrelationReducerWorkBatchQuery(
			now,
			q.LeaseOwner,
			cicdIntents,
		)
		result, err := q.db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf(
				"batch ack reducer work (%d CI/CD items): %w",
				len(cicdIntents),
				err,
			)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("batch ack reducer CI/CD work: rows affected: %w", err)
		}
		if rowsAffected == 0 {
			targetClaimRejected = true
		}
	}

	if len(unrelatedIntents) > 0 {
		args := make([]any, 0, len(unrelatedIntents)+2)
		args = append(args, now, q.LeaseOwner)
		for _, intent := range unrelatedIntents {
			args = append(args, intent.IntentID)
		}

		query := ackReducerWorkBatchQuery(len(unrelatedIntents))

		if _, err := q.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf(
				"batch ack reducer work (%d unrelated items): %w",
				len(unrelatedIntents),
				err,
			)
		}
	}

	if targetClaimRejected {
		return ErrReducerClaimRejected
	}

	return nil
}

func splitReducerAckBatchIntents(
	intents []reducer.Intent,
) ([]reducer.Intent, []reducer.Intent, []reducer.Intent, error) {
	seen := make(map[string]reducer.Intent, len(intents))
	target := make([]reducer.Intent, 0, len(intents))
	cicd := make([]reducer.Intent, 0, len(intents))
	unrelated := make([]reducer.Intent, 0, len(intents))
	for _, intent := range intents {
		if prior, ok := seen[intent.IntentID]; ok {
			if prior.Domain != intent.Domain ||
				prior.ClaimEpoch != intent.ClaimEpoch {
				return nil, nil, nil, fmt.Errorf(
					"batch ack reducer work item %q has conflicting claim epochs or domains",
					intent.IntentID,
				)
			}
			continue
		}
		seen[intent.IntentID] = intent
		if intent.Domain == reducer.DomainContainerImageIdentity {
			target = append(target, intent)
			continue
		}
		if intent.Domain == reducer.DomainCICDRunCorrelation {
			cicd = append(cicd, intent)
			continue
		}
		unrelated = append(unrelated, intent)
	}
	return target, cicd, unrelated, nil
}

func ackContainerImageIdentityReducerWorkBatchQuery(
	now time.Time,
	leaseOwner string,
	intents []reducer.Intent,
) (string, []any) {
	idsByEpoch := make(map[int64][]string)
	for _, intent := range intents {
		idsByEpoch[intent.ClaimEpoch] = append(
			idsByEpoch[intent.ClaimEpoch],
			intent.IntentID,
		)
	}
	epochs := make([]int64, 0, len(idsByEpoch))
	for epoch := range idsByEpoch {
		epochs = append(epochs, epoch)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })

	args := []any{now, leaseOwner}
	predicates := make([]string, 0, len(epochs))
	for _, epoch := range epochs {
		idsPlaceholder := len(args) + 1
		args = append(args, idsByEpoch[epoch])
		epochPlaceholder := len(args) + 1
		args = append(args, epoch)
		predicates = append(predicates, fmt.Sprintf(
			"(work_item_id = ANY($%d::text[]) AND container_image_identity_claim_epoch = $%d)",
			idsPlaceholder,
			epochPlaceholder,
		))
	}

	return `
WITH acknowledged AS MATERIALIZED (
UPDATE fact_work_items AS work
SET status = 'succeeded',
    provenance_edge_identity_upgrade_required = FALSE,
    cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required
            THEN 'succeeded'
        ELSE ''
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN container_image_identity_v3_required
            THEN 'succeeded'
        ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE (` + strings.Join(predicates, " OR ") + `)
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count, status,
    visible_at, created_at, updated_at
)
SELECT 'container_image_identity', count(*)::BIGINT, 'pending',
       emission_clock.emitted_at + INTERVAL '250 milliseconds',
       emission_clock.emitted_at, emission_clock.emitted_at
FROM acknowledged
CROSS JOIN emission_clock
WHERE acknowledged.status = 'succeeded'
GROUP BY emission_clock.emitted_at
ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
    producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
    visible_at = CASE
        WHEN cross_scope_completion_events.status = 'retrying'
            THEN cross_scope_completion_events.visible_at
        ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
    END,
    updated_at = EXCLUDED.updated_at
RETURNING 1
)
SELECT 1 FROM acknowledged LIMIT 1
`, args
}

func ackCICDRunCorrelationReducerWorkBatchQuery(
	now time.Time,
	leaseOwner string,
	intents []reducer.Intent,
) (string, []any) {
	ids := make([]string, 0, len(intents))
	for _, intent := range intents {
		ids = append(ids, intent.IntentID)
	}
	sort.Strings(ids)
	return `
WITH acknowledged AS MATERIALIZED (
UPDATE fact_work_items AS work
SET status = 'succeeded',
    cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work.work_item_id = ANY($3::text[])
  AND stage = 'reducer'
  AND domain = 'ci_cd_run_correlation'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count, status,
    visible_at, created_at, updated_at
)
SELECT 'ci_cd_run_correlation', count(*)::BIGINT, 'pending',
       emission_clock.emitted_at + INTERVAL '250 milliseconds',
       emission_clock.emitted_at, emission_clock.emitted_at
FROM acknowledged
CROSS JOIN emission_clock
WHERE acknowledged.status = 'succeeded'
GROUP BY emission_clock.emitted_at
ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
    producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
    visible_at = CASE
        WHEN cross_scope_completion_events.status = 'retrying'
            THEN cross_scope_completion_events.visible_at
        ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
    END,
    updated_at = EXCLUDED.updated_at
RETURNING 1
)
SELECT 1 FROM acknowledged LIMIT 1
`, []any{now, leaseOwner, ids}
}

func ackReducerWorkBatchQuery(itemCount int) string {
	placeholders := make([]string, itemCount)
	for index := range itemCount {
		placeholders[index] = fmt.Sprintf("$%d", index+3)
	}
	return fmt.Sprintf(`
UPDATE fact_work_items
SET status = 'succeeded',
    provenance_edge_identity_upgrade_required = FALSE,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id IN (%s)
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, strings.Join(placeholders, ", "))
}

// FailBatch marks multiple claimed reducer work items as failed in a single
// round-trip. Each intent is failed with its corresponding error.
func (q ReducerQueue) FailBatch(ctx context.Context, intents []reducer.Intent, causes []error) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()
	for i, intent := range intents {
		cause := causes[i]
		if cause == nil {
			continue
		}
		if err := q.failIntent(ctx, intent, cause); err != nil {
			return fmt.Errorf("batch fail item %d (%s): %w", i, intent.IntentID, err)
		}
	}
	_ = now // used by individual failIntent calls via q.now()
	return nil
}
