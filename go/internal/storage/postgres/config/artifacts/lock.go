// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// repoLockNamespace prefixes every repository-lock key, so the
// config-artifact read model's advisory locks never collide with another
// read model's (the infra inventory uses 'infra_resource_entities:').
const repoLockNamespace = "config_artifact_paths:"

// repoLockSQL takes the repository lock: a transaction-scoped advisory lock
// on the 64-bit hash of repoLockKey(repo_id). It is released at COMMIT or
// ROLLBACK. Send it only through lockRepository.
const repoLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// repoLockKey is the one definition of a repository's lock key text,
// 'config_artifact_paths:' || repo_id.
func repoLockKey(repoID string) string {
	return repoLockNamespace + repoID
}

// lockRepository takes the repository lock as the first statement of tx.
// Every statement that writes a repository's config_artifact_paths rows or
// its config_artifact_repo_state row runs after it in the same transaction:
// the derive's write (protocol section 3, step 3), the failure-only update,
// and the walker's writes when it lands. Taking it first means a state read
// that follows runs on a snapshot taken after the previous holder committed
// (Read Committed). Derives of different repositories never wait on each
// other.
func lockRepository(ctx context.Context, tx db.Executor, repoID string) error {
	if _, err := tx.ExecContext(ctx, repoLockSQL, repoLockKey(repoID)); err != nil {
		return fmt.Errorf("lock config artifacts of %q: %w", repoID, err)
	}
	return nil
}
