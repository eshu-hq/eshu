// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const writerSessionSettingSQL = `SELECT coalesce(current_setting('eshu.infra_inventory_writer', true), '')`

// VerifyWriterSession checks, once at startup, that a content-writer
// binary's pool connection carries the derive-aware writer setting
// (WriterSessionSQL). It returns true when it does. When it does not, for
// example because a connection pooler in front of the DSN dropped the SET,
// it logs postgres.session_unfenced at ERROR and returns false. It never
// fails the binary: correctness does not depend on it, because the fence
// then marks this binary's writes and readers stay on the graph until the
// reducer repairs them. A failed check is logged the same way.
func VerifyWriterSession(ctx context.Context, queryer db.Queryer, logger *slog.Logger) bool {
	setting, err := readWriterSetting(ctx, queryer)
	if err == nil && setting == writerSessionValue {
		return true
	}
	if logger != nil {
		attrs := []any{
			"event_name", "postgres.session_unfenced",
			"setting", "eshu.infra_inventory_writer",
			"value", setting,
			"hint", "a connection pooler must forward eshu.infra_inventory_writer (pgbouncer: track_extra_parameters); " +
				"until it does, this binary's content writes are fenced and infra aggregate reads stay on the graph",
		}
		if err != nil {
			attrs = append(attrs, "error", err)
		}
		logger.ErrorContext(ctx, "postgres session lacks the infra read model writer setting", attrs...)
	}
	return false
}

func readWriterSetting(ctx context.Context, queryer db.Queryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, writerSessionSettingSQL)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var setting string
	if rows.Next() {
		if err := rows.Scan(&setting); err != nil {
			return "", err
		}
	}
	return setting, rows.Err()
}
