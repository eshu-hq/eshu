// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// ghsaWindowCursorReader serves the coordinator's per-type rolling cursor
// probe from the cursor table the extension host advances in its commit
// transactions (#10037).
type ghsaWindowCursorReader struct {
	database db.ExecQueryer
}

func (r ghsaWindowCursorReader) ReadGHSAWindowCursor(ctx context.Context, collectorInstanceID string, advisoryType string) (int64, bool, error) {
	return vulnerabilitystore.NewGHSAWindowCursorStore(r.database).ReadGHSAWindowCursor(ctx, collectorInstanceID, advisoryType)
}
