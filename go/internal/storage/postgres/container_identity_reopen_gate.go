// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The container_image_identity changed-evidence gate (#10878, first domain).
//
// A succeeded container_image_identity item records the identity epoch its
// run read (container_identity_reopen_epochs, migration 318): the count, max
// observed_at, and ordered-set SHA-256 the probe query already returns for
// the active identity set. The deferred-maintenance reopen recomputes the
// epoch once per pass in a MATERIALIZED CTE and re-opens the item only when
// the row is missing, the logic version moved, or any epoch field differs.
// The epoch covers the active identity set fleet-wide, so a quiet fleet
// converges the whole domain to skips; any active-set change moves the
// epoch and re-opens every recorded item, which then re-derives through the
// already-shared once-per-epoch fleet load (IdentityEpochCache).
//
// The epoch CTE reuses probeIdentityEpochQuery verbatim (parenthesized), so
// the gate can never probe a different set than the cache; the
// TestIdentityEpoch* locks on that query keep applying. The max observed_at
// compares as epoch milliseconds: the probe returns timestamptz, the record
// path stores the same instant, and SQL-side NULL (empty set) matches a NULL
// recorded value while a recorded zero time never equals NULL.

// containerIdentityCurrentEpochCTE recomputes the identity epoch once per
// reopen pass. probeIdentityEpochQuery returns exactly one row; the CTE
// header names its columns positionally so the listing never depends on the
// probe's inner aliases.
const containerIdentityCurrentEpochCTE = `, current_epoch(cnt, max_observed, fingerprint) AS MATERIALIZED (
  ` + probeIdentityEpochQuery + `
)
`

// listContainerIdentityReopenCandidatesQuery is
// ListSucceededReducerWorkItemsByDomainQuery for container_image_identity
// with the changed-evidence verdict: the same replay floor,
// failed-generation exclusion, $1 domain line, stage marker, and order,
// plus one input_changed column. The caller re-opens the rows whose
// input_changed is true and counts the rest as skipped_unchanged.
var listContainerIdentityReopenCandidatesQuery = backfill.ScopeReplayFloorCTE +
	containerIdentityCurrentEpochCTE +
	`SELECT work.work_item_id,
       (epoch.work_item_id IS NULL
        OR epoch.logic_version IS DISTINCT FROM ` +
	strconv.Itoa(reducercontract.ContainerImageIdentityLogicVersion) + `
        OR epoch.epoch_count IS DISTINCT FROM current_epoch.cnt
        OR epoch.epoch_max_observed_at IS DISTINCT FROM current_epoch.max_observed
        OR epoch.epoch_fingerprint IS DISTINCT FROM current_epoch.fingerprint) AS input_changed
` +
	backfill.ScopeReplayFloorJoins +
	`CROSS JOIN current_epoch
LEFT JOIN container_identity_reopen_epochs AS epoch
  ON epoch.work_item_id = work.work_item_id
` +
	backfill.ScopeReplayFloorStage +
	`  AND work.domain = $1
` +
	backfill.ScopeReplayFloorBound

// recordContainerIdentityReopenEpochQuery stores the identity epoch of a
// container_image_identity run that is about to succeed. The claimant CTE
// fences it on the exact claim, the same predicate the claim-fenced
// resolution activation uses (work_item_id, last_attempt_at, live
// claim_until), and locks that one work-item row FOR NO KEY UPDATE so a
// concurrent reclaim waits or, if it already committed, drops the row on
// the recheck. The upsert keeps the newest claim: a row recorded by a
// later claim is never replaced by an earlier one. Zero rows affected
// means the claim no longer holds the item.
//
// $1 work_item_id, $2 claimed_at (the claim's last_attempt_at), $3
// epoch_count, $4 epoch_max_observed_at (NULL on an empty set), $5
// epoch_fingerprint, $6 logic_version.
const recordContainerIdentityReopenEpochQuery = `
WITH claimant AS MATERIALIZED (
    SELECT work_item_id
    FROM fact_work_items
    WHERE work_item_id = $1
      AND stage = 'reducer'
      AND domain = 'container_image_identity'
      AND status IN ('claimed', 'running')
      AND last_attempt_at = $2
      AND claim_until > clock_timestamp()
    FOR NO KEY UPDATE
)
INSERT INTO container_identity_reopen_epochs (
    work_item_id, epoch_count, epoch_max_observed_at, epoch_fingerprint,
    logic_version, claimed_at, recorded_at
)
SELECT claimant.work_item_id, $3, $4, $5, $6, $2, clock_timestamp()
FROM claimant
ON CONFLICT (work_item_id) DO UPDATE
SET epoch_count = EXCLUDED.epoch_count,
    epoch_max_observed_at = EXCLUDED.epoch_max_observed_at,
    epoch_fingerprint = EXCLUDED.epoch_fingerprint,
    logic_version = EXCLUDED.logic_version,
    claimed_at = EXCLUDED.claimed_at,
    recorded_at = EXCLUDED.recorded_at
WHERE container_identity_reopen_epochs.claimed_at <= EXCLUDED.claimed_at
`

// ProbeContainerIdentityEpoch returns the identity epoch the next
// container_image_identity run would read: the count, max observed_at, and
// ordered-set fingerprint of the active identity set. MaxObservedAt is nil
// on an empty set. The container handler records this value after its
// durable write so the deferred-maintenance reopen can skip the item while
// the fleet-wide epoch is unchanged (#10878).
func (s *FactStore) ProbeContainerIdentityEpoch(
	ctx context.Context,
) (reducercontract.ContainerIdentityReopenEpoch, error) {
	probe, err := s.probeIdentityEpoch(ctx)
	if err != nil {
		return reducercontract.ContainerIdentityReopenEpoch{}, err
	}
	epoch := reducercontract.ContainerIdentityReopenEpoch{
		EpochCount:       int64(probe.count),
		EpochFingerprint: probe.activeFingerprint,
	}
	if !probe.maxObservedAt.IsZero() {
		at := probe.maxObservedAt.UTC()
		epoch.EpochMaxObservedAt = &at
	}
	return epoch, nil
}

// RecordContainerIdentityReopenEpoch stores the identity epoch of a
// container_image_identity run that is about to succeed (#10878), fenced on
// the exact claim (workItemID plus claimedAt, the claim's last_attempt_at,
// and a live claim_until). It never recomputes the epoch: the value is the
// one ProbeContainerIdentityEpoch returned for the run. It returns an error
// wrapping reducercontract.ErrExecutionClaimRejected when the claim no
// longer holds the item or a later claim already recorded a row.
func (s IngestionStore) RecordContainerIdentityReopenEpoch(
	ctx context.Context,
	workItemID string,
	claimedAt time.Time,
	epoch reducercontract.ContainerIdentityReopenEpoch,
) error {
	var maxObservedAt any
	if epoch.EpochMaxObservedAt != nil {
		maxObservedAt = epoch.EpochMaxObservedAt.UTC()
	}
	result, err := s.database.ExecContext(ctx, recordContainerIdentityReopenEpochQuery,
		workItemID, claimedAt.UTC(), epoch.EpochCount, maxObservedAt,
		epoch.EpochFingerprint, reducercontract.ContainerImageIdentityLogicVersion,
	)
	if err != nil {
		return fmt.Errorf("record container identity reopen epoch: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record container identity reopen epoch: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("record container identity reopen epoch for %s: %w",
			workItemID, reducercontract.ErrExecutionClaimRejected)
	}
	return nil
}

// listContainerIdentityReopenCandidates runs the changed-evidence listing
// and returns the items to re-open plus the count left succeeded (#10878).
func listContainerIdentityReopenCandidates(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
) ([]string, int, error) {
	rows, err := queryer.QueryContext(ctx, listContainerIdentityReopenCandidatesQuery, domain)
	if err != nil {
		return nil, 0, fmt.Errorf("list %s reopen candidates: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()
	reopen := make([]string, 0)
	skipped := 0
	for rows.Next() {
		var workItemID string
		var inputChanged bool
		if err := rows.Scan(&workItemID, &inputChanged); err != nil {
			return nil, 0, fmt.Errorf("scan container_image_identity reopen candidate: %w", err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		if !inputChanged {
			skipped++
			continue
		}
		reopen = append(reopen, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list container_image_identity reopen candidates: %w", err)
	}
	return reopen, skipped, nil
}
