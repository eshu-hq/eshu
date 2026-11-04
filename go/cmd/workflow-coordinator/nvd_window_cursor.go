// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// nvdWindowCursorReader serves the coordinator's rolling cursor probe from
// the cursor table the collector advances in its commit transactions. It
// drops the stored head: the plan needs the cursor only.
type nvdWindowCursorReader struct {
	database db.ExecQueryer
}

func (r nvdWindowCursorReader) ReadNVDWindowCursor(ctx context.Context, collectorInstanceID string) (int64, bool, error) {
	cursor, set, _, err := vulnerabilitystore.NewNVDWindowCursorStore(r.database).ReadNVDWindowCursor(ctx, collectorInstanceID)
	if err != nil {
		return 0, false, err
	}
	return cursor, set, nil
}
