// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"
)

const (
	graphNodeOwnerDefinitionName = "graph_node_owner"
	graphNodeOwnerBatchColumns   = 4
	// graphNodeOwnerAdvisoryPrefix namespaces the per-uid transaction-scoped
	// advisory lock keys so they never collide with another subsystem's
	// advisory locks (e.g. package registry identity).
	graphNodeOwnerAdvisoryPrefix = "eshu:graph_node_owner:"
	maxGraphNodeOwnerAdvisoryKey = uint64(1<<63 - 1)
)

// graphNodeOwnerAcquireLocksSQL acquires one transaction-scoped advisory lock
// per key, in ascending key order. The inner subquery materializes the DISTINCT
// keys sorted BEFORE the lock function is applied, so two concurrent batches
// with overlapping uids always acquire their shared locks in the same order and
// can never deadlock. It is one round-trip regardless of batch size.
const graphNodeOwnerAcquireLocksSQL = `
SELECT pg_advisory_xact_lock(k)
FROM (SELECT DISTINCT k FROM unnest($1::bigint[]) AS t(k) ORDER BY k) s`

// graphNodeOwnerUpsertPrefix / graphNodeOwnerUpsertSuffix build the batched
// atomic max-resolution upsert: a row is overwritten only when the incoming
// source_order_key is strictly greater than the stored one, so Postgres
// resolves the max (observed_at, source_fact_id) contributor per uid.
const graphNodeOwnerUpsertPrefix = `
INSERT INTO graph_node_owner (uid, source_order_key, winning_row, updated_at) VALUES `

const graphNodeOwnerUpsertSuffix = `
ON CONFLICT (uid) DO UPDATE
SET source_order_key = EXCLUDED.source_order_key,
    winning_row = EXCLUDED.winning_row,
    updated_at = EXCLUDED.updated_at
WHERE EXCLUDED.source_order_key > graph_node_owner.source_order_key`

const graphNodeOwnerWinnersSQL = `
SELECT uid, source_order_key
FROM graph_node_owner
WHERE uid = ANY($1::text[])`

// GraphNodeOwnerEntry is one node uid's contribution to the owner ledger: its
// deterministic order key and its full node row (stored as JSONB for the Stage
// 2 provenance foundation; Stage 1 does not read winning_row back for the graph
// write).
type GraphNodeOwnerEntry struct {
	// UID is the canonical graph node uid (globally unique across labels).
	UID string
	// SourceOrderKey is the deterministic max-(observed_at, source_fact_id)
	// order key, encoded so lexicographic string comparison agrees with the
	// intended ordering.
	SourceOrderKey string
	// WinningRow is the JSONB-encoded node row for this contributor.
	WinningRow json.RawMessage
}

// GraphNodeOwnerStore is the Postgres-atomic resolver for #5007 cross-scope
// same-uid node ownership. Its methods operate against a caller-supplied
// transaction (an ExecQueryer that MUST be a live transaction) because the
// advisory locks it acquires have to stay held across the caller's subsequent
// graph write: the owner-ledger decision and the graph write together form the
// per-uid critical section. See
// docs/internal/design/5007-cross-scope-node-ownership.md.
type GraphNodeOwnerStore struct{}

// NewGraphNodeOwnerStore returns a GraphNodeOwnerStore. The store is stateless;
// every method takes the transaction to run against.
func NewGraphNodeOwnerStore() GraphNodeOwnerStore {
	return GraphNodeOwnerStore{}
}

// EnsureSchema applies the graph_node_owner DDL from the embedded migration so
// tests and local flows can create the table without the full bootstrap. The
// migration file is the single source of truth for the DDL.
func (GraphNodeOwnerStore) EnsureSchema(ctx context.Context, ex Executor) error {
	if ex == nil {
		return fmt.Errorf("graph node owner store executor is required")
	}
	ddl, err := graphNodeOwnerSchemaSQL()
	if err != nil {
		return err
	}
	if _, err := ex.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure graph node owner schema: %w", err)
	}
	return nil
}

// graphNodeOwnerSchemaSQL returns the embedded migration DDL for the
// graph_node_owner table, so the store never drifts from the migration.
func graphNodeOwnerSchemaSQL() (string, error) {
	for _, def := range BootstrapDefinitions() {
		if def.Name == graphNodeOwnerDefinitionName {
			return def.SQL, nil
		}
	}
	return "", fmt.Errorf("graph node owner migration %q not found in bootstrap definitions", graphNodeOwnerDefinitionName)
}

// ResolveOwnedUIDs runs the #5007 per-uid critical section against tx: it
// acquires a transaction-scoped advisory lock for every entry's uid (in sorted
// order, one round-trip, deadlock-free), batch-upserts the entries into the
// owner ledger keeping the max order key per uid, reads back the winning order
// key per uid, and returns the set of uids that these entries currently own
// (the entry's SourceOrderKey equals the post-upsert winner). The caller MUST
// then write ONLY the owned uids to the graph and keep tx open across that
// write, committing tx afterward to release the locks.
//
// contendedLost is the number of entries whose uid was won by a different
// (higher-order-key) contributor already in the ledger — i.e. this batch lost
// them to another scope. It is an operator-facing cross-scope-contention signal.
func (s GraphNodeOwnerStore) ResolveOwnedUIDs(
	ctx context.Context,
	tx ExecQueryer,
	entries []GraphNodeOwnerEntry,
	updatedAt time.Time,
) (owned map[string]struct{}, contendedLost int, err error) {
	if tx == nil {
		return nil, 0, fmt.Errorf("graph node owner store transaction is required")
	}
	if updatedAt.IsZero() {
		return nil, 0, fmt.Errorf("graph node owner updated_at is required")
	}
	unique := dedupeOwnerEntries(entries)
	if len(unique) == 0 {
		return map[string]struct{}{}, 0, nil
	}

	if err := s.acquireLocks(ctx, tx, unique); err != nil {
		return nil, 0, err
	}
	if err := s.upsert(ctx, tx, unique, updatedAt.UTC()); err != nil {
		return nil, 0, err
	}
	winners, err := s.winningOrderKeys(ctx, tx, unique)
	if err != nil {
		return nil, 0, err
	}

	owned = make(map[string]struct{}, len(unique))
	for _, entry := range unique {
		win, ok := winners[entry.UID]
		if !ok {
			// No winner row is a contract violation (we just upserted), fail
			// closed rather than silently owning or disowning.
			return nil, 0, fmt.Errorf("graph node owner: no winner row for uid %q after upsert", entry.UID)
		}
		if win == entry.SourceOrderKey {
			owned[entry.UID] = struct{}{}
			continue
		}
		contendedLost++
	}
	return owned, contendedLost, nil
}

func (GraphNodeOwnerStore) acquireLocks(ctx context.Context, tx ExecQueryer, entries []GraphNodeOwnerEntry) error {
	keys := make([]int64, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, graphNodeOwnerAdvisoryKey(entry.UID))
	}
	if _, err := tx.ExecContext(ctx, graphNodeOwnerAcquireLocksSQL, keys); err != nil {
		return fmt.Errorf("acquire graph node owner advisory locks: %w", err)
	}
	return nil
}

func (GraphNodeOwnerStore) upsert(ctx context.Context, tx ExecQueryer, entries []GraphNodeOwnerEntry, updatedAt time.Time) error {
	values := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*graphNodeOwnerBatchColumns)
	for _, entry := range entries {
		base := len(args)
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4))
		row := entry.WinningRow
		if len(row) == 0 {
			row = json.RawMessage("{}")
		}
		args = append(args, entry.UID, entry.SourceOrderKey, []byte(row), updatedAt)
	}
	query := graphNodeOwnerUpsertPrefix + strings.Join(values, ", ") + graphNodeOwnerUpsertSuffix
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert graph node owners: %w", err)
	}
	return nil
}

func (GraphNodeOwnerStore) winningOrderKeys(ctx context.Context, tx ExecQueryer, entries []GraphNodeOwnerEntry) (map[string]string, error) {
	uids := make([]string, 0, len(entries))
	for _, entry := range entries {
		uids = append(uids, entry.UID)
	}
	rows, err := tx.QueryContext(ctx, graphNodeOwnerWinnersSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("read graph node owner winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	winners := make(map[string]string, len(uids))
	for rows.Next() {
		var uid, orderKey string
		if err := rows.Scan(&uid, &orderKey); err != nil {
			return nil, fmt.Errorf("scan graph node owner winner: %w", err)
		}
		winners[uid] = orderKey
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph node owner winners: %w", err)
	}
	return winners, nil
}

// dedupeOwnerEntries drops entries with a blank uid and collapses duplicate
// uids within one batch to the maximum SourceOrderKey (the same within-batch
// tie-break the extractors apply), returning the result sorted by uid so lock
// acquisition and upsert argument order are deterministic.
func dedupeOwnerEntries(entries []GraphNodeOwnerEntry) []GraphNodeOwnerEntry {
	byUID := make(map[string]GraphNodeOwnerEntry, len(entries))
	for _, entry := range entries {
		uid := strings.TrimSpace(entry.UID)
		if uid == "" {
			continue
		}
		entry.UID = uid
		if existing, ok := byUID[uid]; !ok || entry.SourceOrderKey > existing.SourceOrderKey {
			byUID[uid] = entry
		}
	}
	if len(byUID) == 0 {
		return nil
	}
	out := make([]GraphNodeOwnerEntry, 0, len(byUID))
	for _, entry := range byUID {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

// graphNodeOwnerAdvisoryKey derives the deterministic 63-bit advisory lock key
// for a node uid, mirroring PackageRegistryIdentityLocker's fnv-based scheme.
func graphNodeOwnerAdvisoryKey(uid string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(graphNodeOwnerAdvisoryPrefix))
	_, _ = h.Write([]byte(strings.TrimSpace(uid)))
	return int64(h.Sum64() & maxGraphNodeOwnerAdvisoryKey)
}
