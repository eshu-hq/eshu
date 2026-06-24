// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const (
	packageRegistryIdentityAdvisoryLockPrefix = "eshu:package_registry_identity:"
	packageRegistryIdentityAdvisoryLockQuery  = "SELECT pg_advisory_xact_lock($1::bigint)"
	maxPackageRegistryIdentityAdvisoryLockKey = uint64(1<<63 - 1)
	packageRegistryIdentitySlowLockWait       = 100 * time.Millisecond
)

// PackageRegistryIdentityLocker coordinates cross-process graph writes that can
// touch the same Package.uid. Transaction-scoped locks are released by
// Postgres on commit, rollback, or connection loss.
type PackageRegistryIdentityLocker struct {
	DB Beginner
}

// WithPackageRegistryIdentityLocks acquires deterministic advisory locks for
// packageIDs, runs fn, and releases the locks when the transaction ends. Empty
// package ID sets call fn without opening a transaction.
func (l PackageRegistryIdentityLocker) WithPackageRegistryIdentityLocks(
	ctx context.Context,
	packageIDs []string,
	fn func(context.Context) error,
) error {
	if fn == nil {
		return fmt.Errorf("package registry identity lock callback is required")
	}
	lockIDs := uniqueSortedPackageRegistryIdentityIDs(packageIDs)
	if len(lockIDs) == 0 {
		return fn(ctx)
	}
	if l.DB == nil {
		return fmt.Errorf("package registry identity locker database is required")
	}

	tx, err := l.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin package registry identity lock transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	start := time.Now()
	sampleKey := int64(0)
	for index, packageID := range lockIDs {
		key := packageRegistryIdentityAdvisoryLockKey(packageID)
		if index == 0 {
			sampleKey = key
		}
		if _, err := tx.ExecContext(ctx, packageRegistryIdentityAdvisoryLockQuery, key); err != nil {
			return fmt.Errorf("acquire package registry identity lock: %w", err)
		}
	}
	wait := time.Since(start)
	if wait >= packageRegistryIdentitySlowLockWait {
		slog.InfoContext(
			ctx, "package registry identity advisory locks acquired",
			"package_uid_count", len(lockIDs),
			"lock_key_sample", sampleKey,
			"wait_s", wait.Seconds(),
		)
	}

	if err := fn(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit package registry identity lock transaction: %w", err)
	}
	committed = true
	return nil
}

func uniqueSortedPackageRegistryIdentityIDs(packageIDs []string) []string {
	seen := make(map[string]struct{}, len(packageIDs))
	for _, packageID := range packageIDs {
		packageID = strings.TrimSpace(packageID)
		if packageID == "" {
			continue
		}
		seen[packageID] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for packageID := range seen {
		result = append(result, packageID)
	}
	sort.Strings(result)
	return result
}

func packageRegistryIdentityAdvisoryLockKey(packageID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(packageRegistryIdentityAdvisoryLockPrefix))
	_, _ = h.Write([]byte(strings.TrimSpace(packageID)))
	return int64(h.Sum64() & maxPackageRegistryIdentityAdvisoryLockKey)
}
