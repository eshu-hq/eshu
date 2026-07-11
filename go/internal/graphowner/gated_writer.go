// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
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

// Gate resolves #5007 cross-scope node ownership before a graph node write. A
// nil Gate (or one with no ledger wired) writes through unchanged, preserving
// prior behavior on a deployment without the owner ledger — cross-scope
// determinism then depends on the ledger being present, which the reducer wires
// on the Postgres-backed path.
type Gate struct {
	db    postgres.Beginner
	store postgres.GraphNodeOwnerStore
}

// NewGate returns a Gate backed by the owner ledger over db. A nil db yields a
// pass-through gate (no ownership resolution).
func NewGate(db postgres.Beginner) *Gate {
	return &Gate{db: db, store: postgres.NewGraphNodeOwnerStore()}
}

// write runs the per-uid critical section for one batch and delegates the graph
// write of the owned rows to underlying. family names the owning writer for the
// operator contention log.
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

	entries, err := ownerEntriesFromRows(rows)
	if err != nil {
		return err
	}

	tx, err := g.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("graphowner: begin owner ledger transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	owned, contendedLost, err := g.store.ResolveOwnedUIDs(ctx, tx, entries, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("graphowner: resolve owned uids: %w", err)
	}

	ownedRows := filterOwnedRows(rows, owned)
	if err := underlying(ctx, ownedRows, evidenceSource); err != nil {
		// Roll back the ledger upsert so the ledger never records a
		// contribution whose graph write failed (keeps ledger and graph
		// consistent; the reducer retries the whole intent).
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graphowner: commit owner ledger transaction: %w", err)
	}
	committed = true

	logCrossScopeContention(ctx, family, len(rows), len(ownedRows), contendedLost)
	return nil
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
