// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded narrowed wait keys (#11297).
//
// reducer_waiting_items_pkey is a btree over (work_item_id, waits_for_kind,
// waits_for_key). Postgres refuses an index row above roughly 2704 bytes
// (SQLSTATE 54000), and the narrowed corpus key carried the whole verdict-time
// blocking scope set: a fleet with a few hundred holding scopes rendered a key
// of tens of kilobytes, the park INSERT failed inside WorkSink.Fail, and the
// batch worker reads a broken Fail as fatal.
const (
	// waitingIndexRowBudgetBytes caps the whole index row a narrowed wait may
	// build: the work_item_id, the waits_for_kind, and the rendered key. The
	// work_item_id is unbounded text, so it counts against the cap rather than
	// being assumed small. The cap counts JSON text length; Postgres stores the
	// key as JSONB, which costs one more byte per array element than the text,
	// and the index tuple adds its own headers. The measured worst case (a
	// set of 2-character ids, which maximizes elements per byte) is a
	// 2328-byte index row for a 2000-byte counted row, so the real headroom
	// under the 2704-byte btree limit is 376 bytes, not the 704 the
	// arithmetic gap suggests.
	waitingIndexRowBudgetBytes = 2000
	// narrowedWaitingKeyOverheadBytes is the slack reserved for the key's field
	// names, brackets, and separators that the set budgets do not count.
	narrowedWaitingKeyOverheadBytes = 64
	// narrowedWaitingBlockingBudgetBytes bounds the JSON bytes the blocking
	// scope set may add to a narrowed wait key.
	narrowedWaitingBlockingBudgetBytes = 1200
	// narrowedWaitingRepoBudgetBytes bounds the JSON bytes the read-set
	// repository ids may add. No reader matches on them; they only name the
	// read set.
	narrowedWaitingRepoBudgetBytes = 400
)

// Reasons on eshu_dp_reducer_waiting_key_bounded_total (closed set).
const (
	// waitingKeyBoundedReasonCut: the wait parked with its blocking or read
	// set cut to a sorted prefix.
	waitingKeyBoundedReasonCut = "cut"
	// waitingKeyBoundedReasonUnrepresentable: not even one blocking id fits the
	// index-row budget, so nothing parked and the queue's timer path retries.
	waitingKeyBoundedReasonUnrepresentable = "unrepresentable"
	// waitingKeyBoundedReasonIndexRowRejected: Postgres still rejected the park
	// INSERT with SQLSTATE 54000; Fail logged it and returned nil.
	waitingKeyBoundedReasonIndexRowRejected = "index_row_rejected"
)

// postgresProgramLimitExceeded is the SQLSTATE of a btree row over the index
// size limit ("index row size N exceeds btree version 4 maximum 2704").
const postgresProgramLimitExceeded = "54000"

// narrowedWaitKey is the result of rendering one narrowed corpus waits_for key.
type narrowedWaitKey struct {
	// Key is the waits_for_key JSON; empty when Unrepresentable.
	Key string
	// Cut reports that the blocking or read set was cut to a sorted prefix.
	Cut bool
	// Unrepresentable reports that the cause carries a blocking set but not
	// even its first id fits the index-row budget. The caller must park
	// nothing: the coarse {scope_id, generation_id} key is already satisfied
	// when a narrowed deferral is built (the own generation passed its gate),
	// so a coarse park would reopen the waiter in a tight loop while the
	// fence stays closed. The queue row is already retrying on the 10-minute
	// backstop.
	Unrepresentable bool
}

// boundWaitingScopeIDs keeps the leading elements of a normalized (sorted,
// deduplicated) id set whose JSON rendering fits budget bytes. It returns the
// kept prefix, the bytes it uses, and whether anything was cut. The kept set
// is a deterministic sorted prefix, so equal inputs render equal keys and a
// repeated Fail stays idempotent.
//
// A bounded blocking set is a sound wake hint because the prefix is drawn only
// from scopes that hold the fence at verdict time:
//   - the waiter cannot proceed while any prefix member holds, so a wake for a
//     scope outside the prefix would only cost a re-deferral and is never
//     needed;
//   - every re-park draws a fresh prefix from the current holders, so the
//     park-time satisfaction check is false and the wait does not spin;
//   - a prefix that clears without an activation is caught by the audit and
//     the 10-minute backstop, exactly as with the full key;
//   - wakes per waiter are a subset of the full-key wakes, never more.
func boundWaitingScopeIDs(sorted []string, budget int) (kept []string, used int, truncated bool) {
	for i, id := range sorted {
		raw, err := json.Marshal(id)
		if err != nil {
			return sorted[:i], used, true
		}
		next := used + len(raw) + 1 // the element and its separating comma
		if next > budget {
			return sorted[:i], used, true
		}
		used = next
	}
	return sorted, used, false
}

// narrowedWaitingKey renders the narrowed corpus waits_for key for one parked
// intent: the waiter's own scope and generation plus the verdict-time blocking
// scope set and read set the activation wake matches on, both cut to a sorted
// prefix so the whole index row (work_item_id and kind included) stays under
// waitingIndexRowBudgetBytes. ok is false when the inputs carry no narrowing
// (empty scope, generation, or blocking set); a blocking set that cannot be
// represented reports Unrepresentable instead.
func narrowedWaitingKey(scopeID, generationID, workItemID string, blockingScopeIDs, repoIDs []string) (narrowedWaitKey, bool) {
	scope := strings.TrimSpace(scopeID)
	gen := strings.TrimSpace(generationID)
	if scope == "" || gen == "" {
		return narrowedWaitKey{}, false
	}
	allBlocking := normalizeWaitingScopeIDs(blockingScopeIDs)
	if len(allBlocking) == 0 {
		return narrowedWaitKey{}, false
	}
	base, err := json.Marshal(waitingKey{ScopeID: scope, GenerationID: gen})
	if err != nil {
		return narrowedWaitKey{Unrepresentable: true}, true
	}
	room := waitingIndexRowBudgetBytes - len(workItemID) -
		len(WaitingKindRelationshipGenerationActive) - len(base) - narrowedWaitingKeyOverheadBytes
	blocking, blockingUsed, blockingCut := boundWaitingScopeIDs(allBlocking, min(narrowedWaitingBlockingBudgetBytes, room))
	if len(blocking) == 0 {
		return narrowedWaitKey{Unrepresentable: true}, true
	}
	repos, _, reposCut := boundWaitingScopeIDs(normalizeWaitingScopeIDs(repoIDs),
		max(0, min(narrowedWaitingRepoBudgetBytes, room-blockingUsed)))
	raw, err := json.Marshal(waitingKey{
		ScopeID: scope, GenerationID: gen, BlockingScopeIDs: blocking, RepoIDs: repos,
	})
	if err != nil {
		return narrowedWaitKey{Unrepresentable: true}, true
	}
	return narrowedWaitKey{Key: string(raw), Cut: blockingCut || reposCut}, true
}

// recordWaitingKeyBounded counts one bounded narrowed wait on
// eshu_dp_reducer_waiting_key_bounded_total with a closed reason.
func recordWaitingKeyBounded(ctx context.Context, instruments *telemetry.Instruments, reason string) {
	if instruments == nil || instruments.ReducerWaitingKeyBounded == nil {
		return
	}
	instruments.ReducerWaitingKeyBounded.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason(reason)))
}

// waitingIndexRowRejectedError is a park INSERT that Postgres rejected with
// SQLSTATE 54000: the row is too large for the primary-key btree. It is
// deterministic for that key, so retrying the INSERT cannot help; Fail logs,
// counts, and leaves the queue row on its backstop instead of ending the
// process.
type waitingIndexRowRejectedError struct {
	workItemID string
	kind       string
	keyBytes   int
	err        error
}

func (e *waitingIndexRowRejectedError) Error() string {
	return fmt.Sprintf("park waiting reducer work: index row rejected (kind %s, work_item_id %d bytes, key %d bytes): %v",
		e.kind, len(e.workItemID), e.keyBytes, e.err)
}

func (e *waitingIndexRowRejectedError) Unwrap() error { return e.err }

// classifyWaitingInsertError wraps a park INSERT error: only SQLSTATE 54000
// becomes a *waitingIndexRowRejectedError; every other error keeps the
// existing wrapping and reaches the caller unchanged.
func classifyWaitingInsertError(err error, workItemID, kind, keyJSON string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresProgramLimitExceeded {
		return &waitingIndexRowRejectedError{workItemID: workItemID, kind: kind, keyBytes: len(keyJSON), err: err}
	}
	return fmt.Errorf("park waiting reducer work: %w", err)
}

// logWaitingIndexRowRejected records a park the btree rejected: an ERROR an
// operator can alert on, with the work item, wait kind, and key size, plus the
// closed-reason counter. Log fields are identifiers and sizes only.
func (q ReducerQueue) logWaitingIndexRowRejected(ctx context.Context, intent reducercontract.Intent, rejected *waitingIndexRowRejectedError) {
	recordWaitingKeyBounded(ctx, q.Instruments, waitingKeyBoundedReasonIndexRowRejected)
	logger := q.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, "reducer waiting park rejected as an oversized index row; the queue backstop retries the item",
		slog.String("intent_id", intent.IntentID),
		slog.String("waits_for_kind", rejected.kind),
		slog.Int("key_bytes", rejected.keyBytes),
		slog.Int("work_item_id_bytes", len(rejected.workItemID)),
		telemetry.FailureClassAttr("waiting_index_row_rejected"),
		slog.String("error", rejected.err.Error()),
	)
}
