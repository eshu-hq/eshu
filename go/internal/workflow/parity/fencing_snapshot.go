// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// RehearsalInstanceID is the synthetic collector instance the fencing
// snapshot rehearsal seeds under (#9376 criterion 3). The harness declares
// no instance row itself: the satellite declares this instance from its own
// instances file, exactly like the contention harness seed instance it
// names. The value is duplicated here instead of imported so this package
// keeps its import direction (contention imports parity, never the reverse).
const RehearsalInstanceID = "synth-8875-rehearsal-leg-1"

// FencingSnapshotRow is one committed fence maximum per (generation, work
// item): the high-water the rehearsal asserts.
type FencingSnapshotRow struct {
	GenerationID string
	WorkItemID   string
	ClaimID      string
	FencingToken int64
}

// FencingHighWaterKey identifies one fenced writer: fences of different
// work items on one generation are incomparable, so the key always carries
// both.
type FencingHighWaterKey struct {
	GenerationID string
	WorkItemID   string
}

// fencingSnapshotQuery reads the committed fence maxima for one rehearsal
// prefix in deterministic (generation, work item) order, mirroring the
// cutover handover snapshot (scripts/lib/claim-plane-handover-snapshot-fences.sql).
const fencingSnapshotQuery = `SELECT generation_id, work_item_id, claim_id, fencing_token
FROM generation_commit_fences
WHERE work_item_id LIKE $1
ORDER BY generation_id, work_item_id`

// ReadFencingSnapshot returns the committed fence rows for one rehearsal
// item prefix. It is read-only.
func ReadFencingSnapshot(ctx context.Context, queryer db.Queryer, itemPattern string) ([]FencingSnapshotRow, error) {
	rows, err := queryer.QueryContext(ctx, fencingSnapshotQuery, itemPattern)
	if err != nil {
		return nil, fmt.Errorf("read fencing snapshot: %w", err)
	}
	var out []FencingSnapshotRow
	for rows.Next() {
		var row FencingSnapshotRow
		if err := rows.Scan(&row.GenerationID, &row.WorkItemID, &row.ClaimID, &row.FencingToken); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan fencing snapshot: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read fencing snapshot: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close fencing snapshot: %w", err)
	}
	return out, nil
}

// SnapshotDigest renders the snapshot as a sha256 field for the evidence
// record. It follows the contention digest convention: the digest travels
// as a recorded field, never as a constant the code pins, so a deploy
// under a new satellite digest needs no code change.
func SnapshotDigest(rows []FencingSnapshotRow) string {
	sum := sha256.New()
	for _, row := range rows {
		// Hash writes never fail; the blank assignment records the
		// checked return for errcheck.
		_, _ = fmt.Fprintf(sum, "%s\x00%s\x00%s\x00%d\n", row.GenerationID, row.WorkItemID, row.ClaimID, row.FencingToken)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// CheckHighWater requires the snapshot to hold exactly the expected
// committed token per (generation, work item): no stale token below a
// committed one, no missing writer, and no row the rehearsal did not seed.
func CheckHighWater(rows []FencingSnapshotRow, want map[FencingHighWaterKey]int64) error {
	committed := make(map[FencingHighWaterKey]int64, len(rows))
	for _, row := range rows {
		key := FencingHighWaterKey{GenerationID: row.GenerationID, WorkItemID: row.WorkItemID}
		committed[key] = row.FencingToken
	}
	for key, token := range want {
		stored, ok := committed[key]
		switch {
		case !ok:
			return fmt.Errorf("fencing high-water: no committed fence for generation %q work item %q", key.GenerationID, key.WorkItemID)
		case stored != token:
			return fmt.Errorf("fencing high-water: generation %q work item %q committed token %d, want %d",
				key.GenerationID, key.WorkItemID, stored, token)
		}
	}
	for key := range committed {
		if _, ok := want[key]; !ok {
			return fmt.Errorf("fencing high-water: unexpected committed fence for generation %q work item %q",
				key.GenerationID, key.WorkItemID)
		}
	}
	return nil
}

// CheckStaleRace classifies one stale-commit race: the fresh writer's commit
// must have landed (freshErr nil) and the stale writer's must be refused by
// a terminal fence rejection (stale fence, unknown claim, or a fence-row
// refusal). A retryable refusal proves nothing: the writer may still commit.
func CheckStaleRace(freshErr, staleErr error) error {
	if freshErr != nil {
		return fmt.Errorf("fencing race: fresh writer commit failed: %w", freshErr)
	}
	var rejection *fence.Rejection
	if staleErr == nil || !errors.As(staleErr, &rejection) || !rejection.TerminalFailure() {
		return fmt.Errorf("fencing race: stale writer error = %v, want a terminal stale or fence refusal", staleErr)
	}
	switch rejection.Reason {
	case fence.ReasonStaleFence, fence.ReasonNotFound,
		fence.ReasonFenceToken, fence.ReasonFenceClaimMismatch:
		return nil
	default:
		return fmt.Errorf("fencing race: stale writer refused with %q, want a stale or fence refusal", rejection.Reason)
	}
}

// ReplayAmbiguousPin replays one pin after an ambiguous timeout with
// identical arguments and returns the plane's commit id. The plane keeps
// the first commit id and only extends the deadline, so the retry is
// idempotent; a terminal refusal stops at one call. It never falls back to
// an in-tree heartbeat.
func ReplayAmbiguousPin(ctx context.Context, pinner fence.Pinner, code fence.CodeFunc,
	request fence.Request, deadline time.Time,
) (string, error) {
	commitID, err := fence.Pin(ctx, pinner, code, request, deadline, fence.Retry{
		Attempts: 2, AttemptTimeout: time.Second,
	})
	if err != nil {
		return "", fmt.Errorf("replay ambiguous pin: %w", err)
	}
	return commitID, nil
}

// teardownDeletes removes rehearsal rows child-first: facts and fence rows
// before the generations they reference. Every filter carries the run
// prefix, so no other tenant's rows are reachable.
var teardownDeletes = []struct {
	table string
	query string
	arg   func(runID string) string
}{
	{"fact_records", `DELETE FROM fact_records WHERE fact_id LIKE $1`, func(runID string) string {
		return runID + "-%"
	}},
	{"generation_commit_fences", `DELETE FROM generation_commit_fences WHERE work_item_id LIKE $1`, func(runID string) string {
		return runID + "-%"
	}},
	{"scope_generations", `DELETE FROM scope_generations WHERE generation_id LIKE $1`, func(runID string) string {
		return "gen-" + runID + "-%"
	}},
}

// teardownResidueQuery counts every rehearsal row the deletes should have
// removed. A nonzero count refuses the teardown.
const teardownResidueQuery = `SELECT
    (SELECT count(*) FROM fact_records WHERE fact_id LIKE $1)
  + (SELECT count(*) FROM generation_commit_fences WHERE work_item_id LIKE $1)
  + (SELECT count(*) FROM scope_generations WHERE generation_id LIKE $2)`

// TeardownRehearsalRows reverses one rehearsal on the core database: prefix
// deletes child-first, then the residue check. It reports per-table deleted
// counts for the evidence record. It deletes only rows carrying the run
// prefix; satellite-side claim rows go through ResetSatellite, never here.
func TeardownRehearsalRows(ctx context.Context, database db.ExecQueryer, runID string) (map[string]int, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("teardown rehearsal rows: runID must not be blank")
	}
	deleted := make(map[string]int, len(teardownDeletes))
	for _, del := range teardownDeletes {
		result, err := database.ExecContext(ctx, del.query, del.arg(runID))
		if err != nil {
			return nil, fmt.Errorf("teardown %s: %w", del.table, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("teardown %s rows: %w", del.table, err)
		}
		deleted[del.table] = int(n)
	}
	rows, err := database.QueryContext(ctx, teardownResidueQuery, runID+"-%", "gen-"+runID+"-%")
	if err != nil {
		return nil, fmt.Errorf("teardown residue: %w", err)
	}
	var residue int64
	remain := rows.Next()
	if remain {
		if err := rows.Scan(&residue); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan teardown residue: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teardown residue: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("teardown residue: %w", err)
	}
	if residue != 0 {
		return nil, fmt.Errorf("teardown residue: %d rehearsal rows remain", residue)
	}
	return deleted, nil
}
