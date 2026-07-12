// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// lockOnlySlowWaitThreshold is the lock-acquisition duration above which
// writeChunk emits an operator-facing "slow lock wait" log — the 3 AM signal
// that a lock-only chunk is contending with a concurrent Gate-resolved
// base-property write on an overlapping uid set, mirroring
// packageRegistryIdentitySlowLockWait's convention for the same advisory-lock
// primitive.
const lockOnlySlowWaitThreshold = 100 * time.Millisecond

// postureNodeWriteFunc is the shared shape of the #5062 posture/exposure
// property writers' Write*Nodes methods (WriteRDSPostureNodes /
// WriteEC2InternetExposureNodes / WriteEC2BlockDeviceKMSPostureNodes /
// WriteS3InternetExposureNodes): a batch of rows plus the scope/generation/
// evidence-source metadata each writer stamps onto its rows so a later
// Retract* call can remove only its own reducer-owned properties.
type postureNodeWriteFunc func(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error

// postureNodeRetractFunc is the shared shape of the four writers' Retract*
// methods. LockOnlyGate does not wrap retraction — see the doc comment below
// — so wrapper types hold this only to forward it unchanged.
type postureNodeRetractFunc func(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error

// graphNodeOwnerLocker is the narrow surface of postgres.GraphNodeOwnerStore a
// LockOnlyGate needs: acquire the SAME per-uid advisory locks
// ResolveOwnedUIDs uses (postgres.GraphNodeOwnerStore.LockUIDs), with no
// ledger upsert or ownership resolution. It exists so a unit test can
// substitute a fake in-memory locker; postgres.GraphNodeOwnerStore satisfies
// it unchanged.
type graphNodeOwnerLocker interface {
	LockUIDs(ctx context.Context, tx postgres.ExecQueryer, uids []string) error
}

// LockOnlyGate serializes a graph node-property write against the SAME
// per-uid pg_advisory_xact_lock keyspace Gate uses
// (postgres.GraphNodeOwnerStore.LockUIDs is the identical key derivation
// ResolveOwnedUIDs uses), for writers whose rows are NOT order-resolved
// owner-ledger contributors.
//
// #5066 gated the canonical CloudResource/EC2Instance/KubernetesWorkload node
// writers on the #5007 owner ledger so a shared cross-scope node's
// scope-derived properties converge to the max-(observed_at, source_fact_id)
// contributor. The RDS/EC2/S3 posture and internet-exposure property writers
// SET/REMOVE reducer-owned properties on those SAME CloudResource nodes, but
// they are not cross-scope contributors racing for ownership — every scope
// observes the same posture fact for the same underlying resource, so there is
// no "winner" to resolve, only a single deterministic value to stamp. Giving
// them an owner-ledger row would be a category error (they are not
// order-resolved).
//
// This gate is NOT a data-loss fix — prove-theory disproved that premise for
// these writers. Their write shape is an UNCONDITIONAL SET (no WHERE
// compare-and-swap), which NornicDB's OCC handles safely: a concurrent same-uid
// conflict is aborted with Outdated and the production RetryingExecutor
// re-applies it, so a measured 0/100 trials silently lost an update (v1.1.11,
// see docs/internal/design/5007-cross-scope-node-ownership.md). The silent-loss
// NornicDB defect #5062 proved (5-6/100 on v1.1.9..v1.1.11) is specific to the
// WHERE-CONDITIONAL compare-and-swap shape, which these posture writers do not
// use. What the ungated path DOES incur is abort-retry CHURN: two writers
// racing the same uid repeatedly hit the abort-and-retry cycle, measured at
// 3.6-30x per-write latency under forced contention.
//
// LockOnlyGate removes that churn with a pure critical section: acquire the
// uid's advisory lock (the SAME key ResolveOwnedUIDs would acquire for a
// contending base-property write), run the posture writer's graph write while
// holding it, then commit (releasing the lock). This serializes the posture
// write and any concurrent Gate-gated base-property write on the same uid into
// non-overlapping NornicDB transactions, so neither side hits the OCC
// abort-retry cycle, and every writer to a shared CloudResource node uses one
// coordination primitive (matching the #5066 base-property gate). No ledger row
// is written and no ownership is resolved; a lock-only writer always writes its
// own rows.
//
// A nil LockOnlyGate (or one with no db wired) writes through unchanged,
// preserving prior behavior on a deployment without Postgres, exactly like
// Gate.
//
// LockOnlyGate deliberately does NOT wrap Retract*: retraction targets a scope
// (`REMOVE ... WHERE r.<x>_scope_id IN $scope_ids`), not an explicit uid list,
// so there is no row-level uid set to lock ahead of the write the way there is
// for Write*. Wrapper types forward Retract* directly to the underlying
// writer, unchanged from pre-#5062 behavior.
type LockOnlyGate struct {
	db    postgres.Beginner
	store graphNodeOwnerLocker
}

// NewLockOnlyGate returns a LockOnlyGate backed by the same Postgres database
// the #5007 owner ledger uses. A nil db yields a pass-through gate (no
// locking), matching NewGate's pass-through behavior for a deployment without
// Postgres.
func NewLockOnlyGate(db postgres.Beginner) *LockOnlyGate {
	return &LockOnlyGate{db: db, store: postgres.NewGraphNodeOwnerStore()}
}

// write runs the #5062 lock-only critical section over rows in chunks of at
// most lockChunkSize distinct uids (reusing Gate's chunk bound so the
// per-transaction advisory-lock count stays under the same Postgres
// shared-lock budget documented on lockChunkSize), delegating each chunk's
// graph write to underlying while the chunk's uid locks are held. family names
// the owning writer for error context.
func (g *LockOnlyGate) write(
	ctx context.Context,
	family string,
	rows []map[string]any,
	scopeID, generationID, evidenceSource string,
	underlying postureNodeWriteFunc,
) error {
	if len(rows) == 0 {
		return underlying(ctx, rows, scopeID, generationID, evidenceSource)
	}
	if g == nil || g.db == nil {
		// No Postgres wired: write through unchanged. This is the pass-through
		// path, not a serialization workaround — a Postgres-backed reducer
		// always wires the lock-only gate; only a backend without it falls
		// here.
		return underlying(ctx, rows, scopeID, generationID, evidenceSource)
	}

	for start := 0; start < len(rows); start += lockChunkSize {
		end := start + lockChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := g.writeChunk(ctx, family, rows[start:end], scopeID, generationID, evidenceSource, underlying); err != nil {
			return err
		}
	}
	return nil
}

// writeChunk locks every uid in chunk (one Begin, one LockUIDs call acquiring
// at most lockChunkSize advisory locks, one Commit), then runs underlying's
// graph write for the whole chunk while those locks are held, matching
// Gate.writeChunk's transaction shape.
func (g *LockOnlyGate) writeChunk(
	ctx context.Context,
	family string,
	chunk []map[string]any,
	scopeID, generationID, evidenceSource string,
	underlying postureNodeWriteFunc,
) error {
	uids, err := rowUIDs(chunk)
	if err != nil {
		// Fail loud rather than silently write a row without acquiring its lock:
		// a posture write that skipped the advisory lock would defeat the gate.
		return fmt.Errorf("graphowner: lock-only %s: %w", family, err)
	}

	tx, err := g.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("graphowner: begin lock-only transaction for %s: %w", family, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lockStart := time.Now()
	if err := g.store.LockUIDs(ctx, tx, uids); err != nil {
		return fmt.Errorf("graphowner: lock uids for %s: %w", family, err)
	}
	if wait := time.Since(lockStart); wait >= lockOnlySlowWaitThreshold {
		slog.InfoContext(
			ctx, "graph node owner lock-only advisory locks acquired slowly",
			slog.String("family", family),
			slog.Int("uid_count", len(uids)),
			slog.Float64("wait_seconds", wait.Seconds()),
			log.Component("graphowner"),
		)
	}

	if err := underlying(ctx, chunk, scopeID, generationID, evidenceSource); err != nil {
		// Roll back: never commit (and so never release) this chunk's locks
		// over a graph write that failed, and never claim success.
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graphowner: commit lock-only transaction for %s: %w", family, err)
	}
	committed = true
	return nil
}

// rowUIDs extracts the "uid" string field from each row, preserving order and
// duplicates — postgres.GraphNodeOwnerStore.LockUIDs dedupes and drops blanks
// itself, so the lock-only path does not need to repeat that work here.
func rowUIDs(rows []map[string]any) ([]string, error) {
	uids := make([]string, 0, len(rows))
	for i, row := range rows {
		uid, ok := row["uid"].(string)
		if !ok || strings.TrimSpace(uid) == "" {
			// A row with no usable uid cannot be locked, and writing it unlocked
			// would silently defeat the gate. Reject the whole chunk instead.
			return nil, fmt.Errorf("row %d has a missing or non-string uid (%T); every posture row must carry a uid", i, row["uid"])
		}
		uids = append(uids, uid)
	}
	return uids, nil
}
