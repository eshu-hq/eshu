// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// nodeLivenessFunc reports which of the candidate uids are still admitted in
// some scope's current generation. It runs INSIDE the chunk's lock-holding
// Postgres transaction so the check-then-delete sequence is fenced against a
// concurrent re-admit on the same uid: the re-admit's graph write blocks on
// the same per-uid advisory lock until this chunk commits, so it either lands
// before the check (uid reads live, no delete) or after the commit (node
// recreated). Either order converges.
type nodeLivenessFunc func(ctx context.Context, tx db.ExecQueryer, uids []string) (alive map[string]struct{}, err error)

// nodeDeleteFunc deletes dead uids from the graph: the cypher writer retract
// (RetractCloudResourceNodes / RetractEC2InstanceNodes), already bounded to
// sequential UNWIND batches and sequential Execute dispatch.
type nodeDeleteFunc func(ctx context.Context, uids []string, evidenceSource string) error

// RetractDeadUIDs runs the #6887 generation-diff retract critical section
// over candidate uids: per chunk of at most lockChunkSize sorted uids, one
// Postgres transaction acquires that chunk's per-uid advisory locks (the
// IDENTICAL key derivation concurrent writers hold), resolves liveness inside
// the lock, releases the dead uids from the owner ledger, deletes them from
// the graph, and commits — releasing the locks only after the graph delete.
// A chunk whose delete fails rolls back, including its ledger release, so a
// retry re-proves liveness from scratch.
//
// The returned count is dead candidates handed to the graph delete, not
// nodes provably removed: re-issuing the delete for an already-absent node
// (replay, or a concurrent admit that lost the race and recreated nothing)
// succeeds as a no-op and counts again. Operators reconcile the count with
// the graph: the end-state proof is node absence, which the live end-to-end
// test asserts directly.
//
// Correctness invariants (mirroring Gate.write's chunking argument): every
// uid's lock+check+release+delete runs whole inside exactly one chunk's
// transaction, and uids are independent (the ledger release is keyed per-uid
// and the graph delete is uid-anchored), so chunking changes nothing about
// which uids die. The input slice is never mutated: the gate sorts a copy so
// retries and replays converge on the same statement sequence.
//
// A nil Gate (or one with no ledger wired) SKIPS the retract, deliberately
// unlike Gate.write's pass-through: without the ledger there is no per-uid
// lock and no in-transaction live-check, so a delete cannot prove global
// death and must not issue. The failure mode is today's status quo (a stale
// node survives), never an over-delete. Production always wires the ledger;
// this branch exists only for unit tests and miswiring.
func (g *Gate) RetractDeadUIDs(
	ctx context.Context,
	family string,
	uids []string,
	evidenceSource string,
	checkAlive nodeLivenessFunc,
	deleteNodes nodeDeleteFunc,
) (retracted int, err error) {
	if len(uids) == 0 {
		return 0, nil
	}
	if g == nil || g.database == nil {
		return 0, nil
	}
	if checkAlive == nil {
		return 0, fmt.Errorf("graphowner: retract liveness check is required")
	}
	if deleteNodes == nil {
		return 0, fmt.Errorf("graphowner: retract delete function is required")
	}

	sorted := slices.Clone(uids)
	slices.Sort(sorted)

	for start := 0; start < len(sorted); start += lockChunkSize {
		end := start + lockChunkSize
		if end > len(sorted) {
			end = len(sorted)
		}
		chunkStart := time.Now()
		n, err := g.retractChunk(ctx, family, sorted[start:end], evidenceSource, checkAlive, deleteNodes)
		if err != nil {
			return retracted, err
		}
		logRetractChunk(ctx, family, len(sorted[start:end]), n, time.Since(chunkStart))
		retracted += n
	}
	return retracted, nil
}

// logRetractChunk emits the operator-facing per-chunk retract signal: the
// conflict domain (family), how many candidates the generation diff produced,
// how many the global live-check proved dead, and how long the
// lock+check+release+delete critical section held. An all-alive chunk still
// logs (candidates > 0 proves the diff ran); the empty-candidate case never
// reaches here — RetractDeadUIDs returns before opening a transaction.
func logRetractChunk(ctx context.Context, family string, candidates, retracted int, duration time.Duration) {
	slog.InfoContext(
		ctx, "graph node owner retract chunk completed",
		slog.String("family", family),
		slog.Int("candidate_uids", candidates),
		slog.Int("retracted_uids", retracted),
		slog.Duration("duration", duration),
		log.Component("graphowner"),
	)
}

// retractChunk runs the per-uid critical section for one sorted chunk: one
// Begin, one LockUIDs acquiring at most lockChunkSize advisory locks, one
// in-transaction liveness resolution, one ledger release of the dead subset,
// one graph delete of that subset, one Commit. An all-alive chunk still
// commits its lock-holding transaction; an all-alive chunk issues no release
// and no delete.
func (g *Gate) retractChunk(
	ctx context.Context,
	family string,
	chunk []string,
	evidenceSource string,
	checkAlive nodeLivenessFunc,
	deleteNodes nodeDeleteFunc,
) (int, error) {
	tx, err := g.database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("graphowner: begin retract transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := g.store.LockUIDs(ctx, tx, chunk); err != nil {
		return 0, fmt.Errorf("graphowner: lock retract uids: %w", err)
	}
	alive, err := checkAlive(ctx, tx, chunk)
	if err != nil {
		return 0, fmt.Errorf("graphowner: check retract liveness for %s: %w", family, err)
	}
	dead := make([]string, 0, len(chunk))
	for _, uid := range chunk {
		if _, ok := alive[uid]; !ok {
			dead = append(dead, uid)
		}
	}
	if len(dead) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("graphowner: commit empty retract transaction: %w", err)
		}
		committed = true
		return 0, nil
	}

	if err := g.store.ReleaseOwnedUIDs(ctx, tx, dead); err != nil {
		return 0, fmt.Errorf("graphowner: release retract uids: %w", err)
	}
	if err := deleteNodes(ctx, dead, evidenceSource); err != nil {
		// Roll back this chunk's ledger release so the ledger never drops a
		// contribution whose graph delete failed. Earlier chunks already
		// committed and stay committed — the release is idempotent and the
		// graph delete is unconditional, so replaying them on retry
		// reconverges to the same result.
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		// The graph delete above already succeeded while the deferred
		// rollback restores the ledger row: a ledger ghost that blocks
		// re-admission until it is reconciled. Log the uid set so the 3 AM
		// operator can reconcile ledger-vs-graph instead of seeing
		// "nothing to do".
		slog.WarnContext(ctx, "graph node owner retract commit failed after graph delete",
			slog.String("family", family),
			slog.Any("uids", dead),
			log.Component("graphowner"),
		)
		return 0, fmt.Errorf("graphowner: commit retract transaction: %w", err)
	}
	committed = true

	return len(dead), nil
}
