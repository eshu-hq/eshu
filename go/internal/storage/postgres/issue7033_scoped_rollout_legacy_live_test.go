// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build issue7033_rollout

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	issue7033LegacyIndexMigrationPath     = "go/internal/storage/postgres/migrations/130_content_files_relative_path_trgm_index.sql"
	issue7033LegacyIndexMigrationChecksum = "ef395de0a2c1ad86fcbc1f82abcda08c83e689508a1197d6e2848695978a8e41"
	issue7033LegacyLifecycleMigrationPath = "go/internal/storage/postgres/migrations/131_content_files_relative_path_trgm_index_lifecycle.sql"
	issue7033LegacyLifecycleMigrationSum  = "c0494bb1489ca3900f62675aacf0b37cd5524e868ba96a124640f6142b14ef2b"
)

func issue7033ValidateLegacyPathIndexReceipts(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
) error {
	for _, receipt := range []struct {
		name, path, checksum string
	}{
		{"legacy migration 130", issue7033LegacyIndexMigrationPath, issue7033LegacyIndexMigrationChecksum},
		{"legacy migration 131", issue7033LegacyLifecycleMigrationPath, issue7033LegacyLifecycleMigrationSum},
	} {
		var checksum string
		err := queryer.QueryRowContext(ctx, `
SELECT checksum_sha256
FROM eshu_schema_migrations
WHERE path = $1 AND variant = 'full'`, receipt.path).Scan(&checksum)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("untracked relative-path index collides with migration 134: exact legacy 130 and 131 receipts are required")
		}
		if err != nil {
			return fmt.Errorf("read %s receipt: %w", receipt.name, err)
		}
		if checksum != receipt.checksum {
			return fmt.Errorf("%s checksum does not match expected receipt", receipt.name)
		}
	}
	return nil
}
