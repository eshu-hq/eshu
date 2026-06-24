// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
)

const (
	platformGraphAdvisoryLockPrefix = "eshu:platform_graph:"
	platformGraphAdvisoryLockQuery  = "SELECT pg_advisory_xact_lock($1::bigint)"
	maxPlatformGraphAdvisoryLockKey = uint64(1<<63 - 1)
)

// PlatformGraphLocker coordinates cross-process graph writes that can touch
// the same Platform.id. The lock is transaction-scoped so Postgres releases it
// on commit, rollback, or connection loss.
type PlatformGraphLocker struct {
	DB Beginner
}

// WithPlatformLocks acquires deterministic advisory locks for platformIDs,
// runs fn, and releases the locks when the transaction ends. Empty platform ID
// sets call fn without opening a transaction.
func (l PlatformGraphLocker) WithPlatformLocks(
	ctx context.Context,
	platformIDs []string,
	fn func(context.Context) error,
) error {
	if fn == nil {
		return fmt.Errorf("platform graph lock callback is required")
	}
	lockIDs := uniqueSortedPlatformIDs(platformIDs)
	if len(lockIDs) == 0 {
		return fn(ctx)
	}
	if l.DB == nil {
		return fmt.Errorf("platform graph locker database is required")
	}

	tx, err := l.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin platform graph lock transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, platformID := range lockIDs {
		if _, err := tx.ExecContext(ctx, platformGraphAdvisoryLockQuery, platformGraphAdvisoryLockKey(platformID)); err != nil {
			return fmt.Errorf("acquire platform graph lock for %q: %w", platformID, err)
		}
	}

	if err := fn(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit platform graph lock transaction: %w", err)
	}
	committed = true
	return nil
}

func uniqueSortedPlatformIDs(platformIDs []string) []string {
	seen := make(map[string]struct{}, len(platformIDs))
	for _, platformID := range platformIDs {
		platformID = strings.TrimSpace(platformID)
		if platformID == "" {
			continue
		}
		seen[platformID] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for platformID := range seen {
		result = append(result, platformID)
	}
	sort.Strings(result)
	return result
}

func platformGraphAdvisoryLockKey(platformID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(platformGraphAdvisoryLockPrefix))
	_, _ = h.Write([]byte(strings.TrimSpace(platformID)))
	return int64(h.Sum64() & maxPlatformGraphAdvisoryLockKey)
}
