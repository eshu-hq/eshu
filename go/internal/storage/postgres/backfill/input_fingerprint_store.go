// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// InputFingerprintSchemaSQL is the DDL of the reducer_work_input_fingerprints
// side table (#8283). It mirrors migration 192, which the schema Job applies;
// TestDDLMigrationParity keeps the two in step.
//
// The fingerprint column holds a SHA-256 over per-generation resolved-row
// COUNTS as well as generation ids and activation stamps, and that is the
// reason the count is part of the key: the cross-repo resolver does not insert
// rows and activate in one transaction. It inserts resolved rows one autocommit
// statement at a time under the scope's own generation id (step 4,
// ON CONFLICT DO NOTHING), writes intents (step 5), and only then activates the
// generation in a separate transaction (step 6, cross_repo_resolution.go). A
// replay can therefore add rows to an already-active generation before
// activated_at moves, and a death between steps 4 and 6 leaves them unstamped.
// Rows only grow (TestResolvedRelationshipsStayAppendOnly guards that), so a
// changed count is a sound signal where the stamp alone is not. The foreign key cascades, so
// resetting a scope generation (which deletes its succeeded reducer work
// items) removes the rows with them; each success overwrites its row.
const InputFingerprintSchemaSQL = `
CREATE TABLE IF NOT EXISTS reducer_work_input_fingerprints (
    work_item_id TEXT PRIMARY KEY REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    repo_ids TEXT[] NOT NULL,
    fingerprint TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
`

// UpsertInputFingerprintSQL records one work item's input fingerprint. The
// upsert is keyed on the work item id, so a retry after a failed ACK, a replay,
// and a second success all overwrite the same row: the row always describes the
// newest successful read.
const UpsertInputFingerprintSQL = `
INSERT INTO reducer_work_input_fingerprints
    (work_item_id, domain, repo_ids, fingerprint, recorded_at)
VALUES ($1, $2, $3::text[], $4, $5)
ON CONFLICT (work_item_id) DO UPDATE
SET domain = EXCLUDED.domain,
    repo_ids = EXCLUDED.repo_ids,
    fingerprint = EXCLUDED.fingerprint,
    recorded_at = EXCLUDED.recorded_at
`

// InputFingerprintStore persists the per-work-item input fingerprints the
// deferred-maintenance reopen compares against (#8283). It implements
// reducer.DeployableUnitInputFingerprintWriter.
type InputFingerprintStore struct {
	database db.Executor
	// Now supplies recorded_at; tests inject a fixed clock. Nil uses time.Now.
	Now func() time.Time
}

// NewInputFingerprintStore builds the store over an executor.
func NewInputFingerprintStore(database db.Executor) InputFingerprintStore {
	return InputFingerprintStore{database: database}
}

// EnsureSchema creates the side table if it does not exist.
func (s InputFingerprintStore) EnsureSchema(ctx context.Context) error {
	if s.database == nil {
		return fmt.Errorf("input fingerprint store database is required")
	}
	if _, err := s.database.ExecContext(ctx, InputFingerprintSchemaSQL); err != nil {
		return fmt.Errorf("ensure input fingerprint schema: %w", err)
	}
	return nil
}

// RecordDeployableUnitInputFingerprint upserts the fingerprint a
// deployable_unit_correlation item read, with the repository ids that read
// used. The reopen listing recomputes the value from these stored ids, never
// from the work item payload. The fingerprint is stored verbatim: it is the
// string the fused read computed in SQL.
func (s InputFingerprintStore) RecordDeployableUnitInputFingerprint(
	ctx context.Context,
	workItemID string,
	repoIDs []string,
	fingerprint string,
) error {
	if s.database == nil {
		return fmt.Errorf("input fingerprint store database is required")
	}
	workItemID = strings.TrimSpace(workItemID)
	if workItemID == "" || fingerprint == "" {
		return fmt.Errorf("record input fingerprint: work item id and fingerprint are required")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	stored := uniqueNonEmptyRepoIDs(repoIDs)
	if _, err := s.database.ExecContext(
		ctx, UpsertInputFingerprintSQL,
		workItemID, deployableUnitInputFingerprintDomain, array.StringArray(stored), fingerprint, now().UTC(),
	); err != nil {
		return fmt.Errorf("record input fingerprint for work item %q: %w", workItemID, err)
	}
	return nil
}

// deployableUnitInputFingerprintDomain is the domain column of the rows the
// deployable_unit_correlation handler records.
const deployableUnitInputFingerprintDomain = "deployable_unit_correlation"

// uniqueNonEmptyRepoIDs trims, drops blanks, and de-duplicates repository ids,
// keeping first-seen order. It never returns nil: an empty set is stored as an
// empty array, not SQL NULL.
func uniqueNonEmptyRepoIDs(repoIDs []string) []string {
	seen := make(map[string]struct{}, len(repoIDs))
	out := make([]string, 0, len(repoIDs))
	for _, id := range repoIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
