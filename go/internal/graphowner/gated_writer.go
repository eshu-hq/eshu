// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// nodeBatchWriteFunc is the underlying graph node write for one batch of rows,
// the shared shape of every family's cypher writer method
// (WriteCloudResourceNodes / WriteEC2InstanceNodes / WriteKubernetesWorkloadNodes).
type nodeBatchWriteFunc func(ctx context.Context, rows []map[string]any, evidenceSource string) error

// sourceOrderKeyRowField is the node-row map key the reducer row builders stamp
// with the deterministic (observed_at, source_fact_id) order key, and the gate
// reads to resolve ownership.
const sourceOrderKeyRowField = "source_order_key"

// lockChunkSize bounds the number of distinct uids resolved under one
// owner-ledger transaction. Postgres's shared advisory-lock table defaults to
// approximately max_locks_per_transaction * (max_connections +
// max_prepared_transactions) slots — about 6400 on Postgres 18's stock
// defaults (max_locks_per_transaction=64, max_connections=100). #5007 P2-1
// proved that one transaction resolving an entire materialization intent's
// rows (unbounded: loadFactsForKinds -> ListFactsByKind carries no LIMIT, so a
// large cloud-account scope can be tens of thousands of uids) can exhaust that
// shared memory outright: 20000 pg_advisory_xact_lock acquisitions in one
// transaction failed with "ERROR: out of shared memory" against a default
// Postgres 18 instance (see
// docs/internal/design/5007-cross-scope-node-ownership.md). Bounding every
// transaction to lockChunkSize keeps per-tx lock count far under the
// cluster-wide budget even with several reducer workers writing concurrently,
// and reuses cypher.DefaultBatchSize so the owner-ledger chunk boundary lines
// up with the graph writer's own MERGE batch size.
const lockChunkSize = cypher.DefaultBatchSize

// graphNodeOwnerResolver is the narrow surface of postgres.GraphNodeOwnerStore
// the gate needs. It exists so a unit test can substitute a fake in-memory
// store instead of a live Postgres transaction; postgres.GraphNodeOwnerStore
// satisfies it unchanged.
type graphNodeOwnerResolver interface {
	ResolveOwnedUIDs(
		ctx context.Context,
		tx postgres.ExecQueryer,
		entries []postgres.GraphNodeOwnerEntry,
		updatedAt time.Time,
	) (owned map[string]struct{}, contendedLost int, err error)
}

// Gate resolves #5007 cross-scope node ownership before a graph node write. A
// nil Gate (or one with no ledger wired) writes through unchanged, preserving
// prior behavior on a deployment without the owner ledger — cross-scope
// determinism then depends on the ledger being present, which the reducer wires
// on the Postgres-backed path.
type Gate struct {
	db    postgres.Beginner
	store graphNodeOwnerResolver

	// Instruments records the #5007 cross-scope ownership contention counter
	// (eshu_dp_cross_scope_ownership_contended_rows_total). Optional: nil
	// skips the metric and keeps the existing structured-log signal, matching
	// the sibling cypher writers' Instruments field convention (e.g.
	// storage/cypher.EdgeWriter.Instruments). Set as a public field after
	// NewGate, not a constructor parameter, mirroring that same convention.
	Instruments *telemetry.Instruments
}

// NewGate returns a Gate backed by the owner ledger over db. A nil db yields a
// pass-through gate (no ownership resolution).
func NewGate(db postgres.Beginner) *Gate {
	return &Gate{db: db, store: postgres.NewGraphNodeOwnerStore()}
}

// write runs the #5007 per-uid critical section over rows in chunks of at
// most lockChunkSize distinct uids, delegating the graph write of each
// chunk's owned rows to underlying, and emits one aggregated contention
// signal for the whole intent after every chunk has committed. family names
// the owning writer for the operator contention log and metric.
//
// Correctness invariant (why chunking is safe): rows arrives already deduped
// to one row per uid by the upstream Extract*NodeRows byUID map, so slicing
// rows into chunks never splits a single uid's lock+upsert+winner resolution
// across two transactions — each uid's critical section still runs whole,
// inside exactly one chunk's transaction. Every uid's ownership decision is
// independent of every other uid's (the ledger upsert is keyed and locked
// per-uid), so resolving different uids under different transactions changes
// nothing about which contributor wins each uid: converge-to-max holds
// identically whether the whole intent runs in one transaction or many. A
// failure partway through the loop leaves earlier chunks committed and
// returns the error to the reducer, which retries the whole intent; the
// ledger's max-upsert is monotonic (a lower order key can never overwrite a
// higher one) and the graph MERGE is idempotent, so replaying already-owned
// uids on retry reconverges to the same result — partial progress from a
// mid-intent failure is safe, not a correctness hazard. This is NOT
// serialization: each uid's advisory lock is held only for its own chunk's
// transaction (released at that chunk's commit), so total lock hold time
// drops and cross-scope concurrency is preserved and improved, not reduced.
func (g *Gate) write(
	ctx context.Context,
	family string,
	rows []map[string]any,
	evidenceSource string,
	underlying nodeBatchWriteFunc,
) error {
	if len(rows) == 0 {
		return underlying(ctx, rows, evidenceSource)
	}
	if g == nil || g.db == nil {
		// No ledger wired: write through unchanged. This is the pass-through
		// path, not a serialization workaround — a Postgres-backed reducer
		// always wires the ledger; only a backend without it falls here.
		return underlying(ctx, rows, evidenceSource)
	}

	var totalOwned, totalContendedLost int
	for start := 0; start < len(rows); start += lockChunkSize {
		end := start + lockChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		owned, contendedLost, err := g.writeChunk(ctx, rows[start:end], evidenceSource, underlying)
		if err != nil {
			return err
		}
		totalOwned += owned
		totalContendedLost += contendedLost
	}

	logCrossScopeContention(ctx, family, len(rows), totalOwned, totalContendedLost)
	recordCrossScopeContention(ctx, g.Instruments, family, totalContendedLost)
	return nil
}

// writeChunk runs the per-uid critical section for one chunk of rows (at most
// lockChunkSize distinct uids: one Begin, one ResolveOwnedUIDs call acquiring
// at most lockChunkSize advisory locks, one Commit) and delegates the graph
// write of the chunk's owned subset to underlying. It returns the number of
// rows this chunk owned and the number it lost to a higher-order-key
// contributor, so write can accumulate totals across chunks.
func (g *Gate) writeChunk(
	ctx context.Context,
	chunk []map[string]any,
	evidenceSource string,
	underlying nodeBatchWriteFunc,
) (owned int, contendedLost int, err error) {
	entries, err := ownerEntriesFromRows(chunk)
	if err != nil {
		return 0, 0, err
	}

	tx, err := g.db.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("graphowner: begin owner ledger transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	ownedUIDs, contendedLost, err := g.store.ResolveOwnedUIDs(ctx, tx, entries, time.Now().UTC())
	if err != nil {
		return 0, 0, fmt.Errorf("graphowner: resolve owned uids: %w", err)
	}

	ownedRows := filterOwnedRows(chunk, ownedUIDs)
	if err := underlying(ctx, ownedRows, evidenceSource); err != nil {
		// Roll back this chunk's ledger upsert so the ledger never records a
		// contribution whose graph write failed. Earlier chunks in this
		// intent already committed and stay committed — see the
		// correctness-invariant comment on write for why that partial
		// progress is safe under the reducer's whole-intent retry.
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("graphowner: commit owner ledger transaction: %w", err)
	}
	committed = true

	return len(ownedRows), contendedLost, nil
}

// ownerEntriesFromRows builds one owner-ledger entry per row, carrying the row's
// uid, its deterministic order key, and its full row as JSONB (the Stage 2
// provenance foundation). A row missing a string uid or order key is a
// programmer error upstream; the entry still carries the empty value so the
// ledger's max resolution treats it consistently (an empty order key loses to
// any real one), and the store's dedupe drops blank uids.
func ownerEntriesFromRows(rows []map[string]any) ([]postgres.GraphNodeOwnerEntry, error) {
	entries := make([]postgres.GraphNodeOwnerEntry, 0, len(rows))
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		orderKey, _ := row[sourceOrderKeyRowField].(string)
		raw, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("graphowner: marshal owner row for uid %q: %w", uid, err)
		}
		entries = append(entries, postgres.GraphNodeOwnerEntry{
			UID:            uid,
			SourceOrderKey: orderKey,
			WinningRow:     raw,
		})
	}
	return entries, nil
}

// filterOwnedRows returns the subset of rows whose uid this batch currently
// owns, preserving input order. Rows this batch lost to a higher-order-key
// contributor are skipped: the winning contributor writes them under the same
// per-uid lock, so the final graph node is always the max contributor's row.
func filterOwnedRows(rows []map[string]any, owned map[string]struct{}) []map[string]any {
	if len(owned) == len(rows) {
		// Common non-contended case: this batch owns every uid, so the graph
		// write is byte-identical to the un-gated write (its own rows).
		allOwned := true
		for _, row := range rows {
			uid, _ := row["uid"].(string)
			if _, ok := owned[uid]; !ok {
				allOwned = false
				break
			}
		}
		if allOwned {
			return rows
		}
	}
	ownedRows := make([]map[string]any, 0, len(owned))
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		if _, ok := owned[uid]; ok {
			ownedRows = append(ownedRows, row)
		}
	}
	return ownedRows
}

// logCrossScopeContention emits an operator-facing signal when this batch lost
// any uid to a higher-order-key contributor from another scope — the 3 AM
// evidence that cross-scope same-uid contention is occurring and being resolved
// deterministically. Silent when the batch owned everything (the common case).
func logCrossScopeContention(ctx context.Context, family string, total, owned, contendedLost int) {
	if contendedLost == 0 {
		return
	}
	slog.InfoContext(
		ctx, "graph node owner cross-scope contention resolved",
		slog.String("family", family),
		slog.Int("batch_rows", total),
		slog.Int("owned_rows", owned),
		slog.Int("contended_lost", contendedLost),
		log.Component("graphowner"),
	)
}

// recordCrossScopeContention increments the #5007 cross-scope ownership
// contention counter (eshu_dp_cross_scope_ownership_contended_rows_total) by
// contendedLost for family, the operator-facing dashboard signal alongside
// logCrossScopeContention's structured log. A nil instruments (a Gate wired
// without telemetry) or a zero contendedLost is a silent no-op — the metric
// only ever records real cross-scope contention, matching the log's
// non-contended-is-silent convention.
func recordCrossScopeContention(ctx context.Context, instruments *telemetry.Instruments, family string, contendedLost int) {
	if contendedLost == 0 || instruments == nil || instruments.CrossScopeOwnershipContendedRows == nil {
		return
	}
	instruments.CrossScopeOwnershipContendedRows.Add(ctx, int64(contendedLost), metric.WithAttributes(
		telemetry.AttrOwnershipFamily(family),
	))
}
