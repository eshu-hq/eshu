// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// ghsaWindowCursorReader serves the coordinator's rolling cursor probe from
// the per-advisory-type cursor table the collector advances in its commit
// transactions (eshu-hq/eshu#10517). It reports both advisory types every
// read, set or not, so the plan bootstraps an unseen stream from the
// planner lookback while planning a seen one from its cursor.
type ghsaWindowCursorReader struct {
	database db.ExecQueryer
}

func (r ghsaWindowCursorReader) ReadGHSAWindowCursors(ctx context.Context, collectorInstanceID string) (map[string]vulnerability.GHSAWindowCursor, error) {
	store := vulnerabilitystore.NewGHSAWindowCursorStore(r.database)
	cursors := make(map[string]vulnerability.GHSAWindowCursor, 2)
	for _, advisoryType := range []string{vulnsource.GHSAAdvisoryTypeReviewed, vulnsource.GHSAAdvisoryTypeMalware} {
		cursor, set, err := store.ReadGHSAWindowCursor(ctx, collectorInstanceID, advisoryType)
		if err != nil {
			return nil, err
		}
		cursors[advisoryType] = vulnerability.GHSAWindowCursor{UnixMicro: cursor, Set: set}
	}
	return cursors, nil
}
