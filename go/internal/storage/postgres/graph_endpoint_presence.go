// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	graphEndpointPresenceBatchSize     = 250
	graphEndpointPresenceColumnsPerRow = 6
)

// graphEndpointPresenceSchemaSQL is the durable uid-exact endpoint-presence
// table (issue #1380, ADR #1314 §6/§8). One row per committed endpoint node uid,
// keyed by (keyspace, uid) so a cross-scope projection can ask "is node X
// committed?" — a question graph_projection_phase_state cannot express because it
// is keyed by scope_id+generation_id. The scope_id FK with ON DELETE CASCADE ties
// a presence row to the scope that committed it so a scope drop removes presence
// alongside the nodes.
const graphEndpointPresenceSchemaSQL = `
CREATE TABLE IF NOT EXISTS graph_endpoint_presence (
    keyspace TEXT NOT NULL,
    uid TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL DEFAULT '',
    source_generation TEXT NOT NULL DEFAULT '',
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (keyspace, uid)
);
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS source_generation TEXT NOT NULL DEFAULT '';
-- One-time, NON-DESTRUCTIVE backfill of pre-#2842 repo_workload presence rows. On
-- an upgraded database the ADD COLUMN ... DEFAULT '' above leaves legacy
-- api_endpoint_repo_path / repo_workload rows with a blank repo_id, which the
-- repo-scoped stale-row delete (keyed repo_id = ANY(...)) can never match. For the
-- repo_workload keyspace the uid IS the bare repo_id, so we recover it here. We do
-- NOT delete: deleting a blank-provenance row would remove still-CURRENT target
-- presence too, and because filterRowsByReadiness terminalizes (does not defer) a
-- handles_route/runs_in row whose presence is absent, that would silently drop a
-- live edge until the next re-materialization (#2842/#2903 review). After this
-- backfill, the runtime retract (RetractStaleRepoGenerations, #2896 path that
-- covers every scope repo) deletes the legacy row only when its repo
-- re-materializes with a different generation, while a still-current row is
-- re-upserted with the live generation first and survives. The api_endpoint_repo_path
-- uid is a SHA-256 hash (#2844), so repo_id is unrecoverable there; those legacy
-- rows are left in place and are bounded-safe — the HANDLES_ROUTE MERGE re-MATCHes
-- the actual :Endpoint node, so a stale-present row never creates an edge to a
-- removed or re-pathed endpoint, and a current endpoint re-upserts proper
-- provenance on its next materialization. The predicate matches nothing once
-- migrated, so it is idempotent on every EnsureSchema.
UPDATE graph_endpoint_presence
SET repo_id = uid
WHERE keyspace = 'repo_workload'
  AND repo_id = ''
  AND uid <> '';
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_scope_idx
    ON graph_endpoint_presence (scope_id);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_updated_idx
    ON graph_endpoint_presence (updated_at DESC);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_stale_idx
    ON graph_endpoint_presence (keyspace, scope_id, repo_id);
`

const upsertGraphEndpointPresenceBatchPrefix = `
INSERT INTO graph_endpoint_presence (
    keyspace, uid, scope_id, repo_id, source_generation, committed_at, updated_at
) VALUES `

// upsertGraphEndpointPresenceBatchSuffix makes the upsert idempotent: a repeated
// (keyspace, uid) converges on one row and only refreshes the commit/update
// instants, the owning scope, and the repo_id/source_generation provenance, so
// concurrent materializer workers and reducer retries never duplicate or
// fabricate presence.
const upsertGraphEndpointPresenceBatchSuffix = `
ON CONFLICT (keyspace, uid) DO UPDATE
SET scope_id = EXCLUDED.scope_id,
    repo_id = EXCLUDED.repo_id,
    source_generation = EXCLUDED.source_generation,
    committed_at = EXCLUDED.committed_at,
    updated_at = EXCLUDED.updated_at
`

const retractGraphEndpointPresenceByScopeSQL = `
DELETE FROM graph_endpoint_presence
WHERE scope_id = ANY($1)
`

// retractStaleGraphEndpointPresenceSQL removes a keyspace's presence rows for the
// listed repos whose source_generation differs from the current generation
// (#2842). It is race-free: it deletes only OTHER generations' rows, never the
// current generation's rows a sibling intent may have just upserted, so
// concurrent materializer workers for the same scope cannot delete each other's
// in-flight presence; deleting an already-removed older row is idempotent.
const retractStaleGraphEndpointPresenceSQL = `
DELETE FROM graph_endpoint_presence
WHERE keyspace = $1
  AND scope_id = $2
  AND repo_id = ANY($3::text[])
  AND source_generation <> $4
`

// presentGraphEndpointUIDsSQL is the single bounded lookup the gate runs per
// keyspace. It returns the present subset of the candidate uids; the store
// computes the missing set in memory. There is no per-uid query, so the gate
// cost is one round trip per keyspace regardless of uid count.
const presentGraphEndpointUIDsSQL = `
SELECT uid
FROM graph_endpoint_presence
WHERE keyspace = $1
  AND uid = ANY($2::text[])
`

// GraphEndpointPresenceStore persists uid-exact endpoint-node presence in
// PostgreSQL. It backs the cross-scope readiness gate for the secrets/IAM graph
// projection (issue #1380): the CloudResource and KubernetesWorkload node
// materializers upsert presence per committed node uid, and the projection gate
// reads it through MissingUIDs.
type GraphEndpointPresenceStore struct {
	db ExecQueryer
}

// NewGraphEndpointPresenceStore constructs a store backed by the provided
// database handle.
func NewGraphEndpointPresenceStore(db ExecQueryer) *GraphEndpointPresenceStore {
	return &GraphEndpointPresenceStore{db: db}
}

// GraphEndpointPresenceSchemaSQL returns the DDL for the endpoint-presence table.
func GraphEndpointPresenceSchemaSQL() string {
	return graphEndpointPresenceSchemaSQL
}

// EnsureSchema applies the endpoint-presence DDL.
func (s *GraphEndpointPresenceStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, graphEndpointPresenceSchemaSQL)
	return err
}

// Upsert writes endpoint-presence rows in idempotent batches. Rows with a blank
// keyspace, uid, or scope_id are skipped because they cannot key a presence row;
// an empty input is a no-op.
func (s *GraphEndpointPresenceStore) Upsert(ctx context.Context, rows []reducer.EndpointPresenceRow) error {
	if len(rows) == 0 {
		return nil
	}

	cleaned := make([]reducer.EndpointPresenceRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(string(row.Keyspace)) == "" ||
			strings.TrimSpace(row.UID) == "" ||
			strings.TrimSpace(row.ScopeID) == "" {
			continue
		}
		cleaned = append(cleaned, row)
	}
	if len(cleaned) == 0 {
		return nil
	}

	for i := 0; i < len(cleaned); i += graphEndpointPresenceBatchSize {
		end := i + graphEndpointPresenceBatchSize
		if end > len(cleaned) {
			end = len(cleaned)
		}
		if err := upsertGraphEndpointPresenceBatch(ctx, s.db, cleaned[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// RetractScope deletes every presence row owned by the given scopes so a node
// retract removes its presence. The scope FK's ON DELETE CASCADE already covers a
// full scope drop; this path covers an in-scope retract that keeps the scope row.
// Blank scope ids are dropped and an empty input is a no-op.
func (s *GraphEndpointPresenceStore) RetractScope(ctx context.Context, scopeIDs []string) error {
	cleaned := cleanStringFilterValues(scopeIDs)
	if len(cleaned) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, retractGraphEndpointPresenceByScopeSQL, cleaned); err != nil {
		return fmt.Errorf("retract graph endpoint presence for %d scope(s): %w", len(cleaned), err)
	}
	return nil
}

// RetractStaleRepoGenerations removes the keyspace's presence rows for the given
// repos whose source_generation differs from generationID (#2842). It deletes
// only OTHER generations' rows for those repos, so a sibling materializer worker
// that just upserted the current generation's rows for an overlapping repo is
// never disturbed; the delete is idempotent. A blank keyspace, scope, or
// generation, or an empty repo set, is a no-op (the gate stays at its pre-#2842
// behavior, only growing the table).
func (s *GraphEndpointPresenceStore) RetractStaleRepoGenerations(
	ctx context.Context,
	keyspace reducer.GraphProjectionKeyspace,
	scopeID, generationID string,
	repoIDs []string,
) error {
	keyspaceValue := strings.TrimSpace(string(keyspace))
	scope := strings.TrimSpace(scopeID)
	generation := strings.TrimSpace(generationID)
	repos := cleanStringFilterValues(repoIDs)
	if keyspaceValue == "" || scope == "" || generation == "" || len(repos) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(
		ctx, retractStaleGraphEndpointPresenceSQL, keyspaceValue, scope, repos, generation,
	); err != nil {
		return fmt.Errorf(
			"retract stale graph endpoint presence for keyspace %q (%d repo(s)): %w",
			keyspaceValue, len(repos), err,
		)
	}
	return nil
}

// MissingUIDs returns the candidate uids that have no presence row for the
// keyspace, using one bounded query plus an in-memory set-difference. Duplicate
// and blank candidates are normalized away first so the bound matches the
// distinct query inputs; an empty candidate set yields an empty result with no
// query.
func (s *GraphEndpointPresenceStore) MissingUIDs(
	ctx context.Context,
	keyspace reducer.GraphProjectionKeyspace,
	uids []string,
) ([]string, error) {
	keyspaceValue := strings.TrimSpace(string(keyspace))
	if keyspaceValue == "" {
		return nil, fmt.Errorf("graph endpoint presence lookup requires a keyspace")
	}

	distinct := cleanStringFilterValues(uids)
	if len(distinct) == 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, presentGraphEndpointUIDsSQL, keyspaceValue, distinct)
	if err != nil {
		return nil, fmt.Errorf("query graph endpoint presence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]struct{}, len(distinct))
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan graph endpoint presence uid: %w", err)
		}
		present[uid] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph endpoint presence: %w", err)
	}

	missing := make([]string, 0, len(distinct))
	for _, uid := range distinct {
		if _, ok := present[uid]; !ok {
			missing = append(missing, uid)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	return missing, nil
}

func upsertGraphEndpointPresenceBatch(ctx context.Context, db ExecQueryer, batch []reducer.EndpointPresenceRow) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*graphEndpointPresenceColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		committedAt := row.CommittedAt.UTC()
		if committedAt.IsZero() {
			committedAt = time.Now().UTC()
		}

		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * graphEndpointPresenceColumnsPerRow
		// committed_at and updated_at both bind the same committedAt arg ($6).
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+6,
		)
		args = append(
			args,
			strings.TrimSpace(string(row.Keyspace)),
			strings.TrimSpace(row.UID),
			strings.TrimSpace(row.ScopeID),
			strings.TrimSpace(row.RepoID),
			strings.TrimSpace(row.SourceGeneration),
			committedAt,
		)
	}

	query := upsertGraphEndpointPresenceBatchPrefix + values.String() + upsertGraphEndpointPresenceBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert graph endpoint presence batch (%d rows): %w", len(batch), err)
	}
	return nil
}
