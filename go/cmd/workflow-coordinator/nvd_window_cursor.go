// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// nvdWindowCursorReader serves the coordinator's rolling cursor probe from
// the cursor table the collector advances in its commit transactions. The
// plan reads the cursor; the head and update time ride along as the
// coordinator-visible lag evidence (eshu-hq/eshu#10716).
type nvdWindowCursorReader struct {
	database db.ExecQueryer
}

func (r nvdWindowCursorReader) ReadNVDWindowCursor(ctx context.Context, collectorInstanceID string) (vulnerability.NVDWindowCursor, error) {
	state, err := vulnerabilitystore.NewNVDWindowCursorStore(r.database).ReadNVDWindowCursor(ctx, collectorInstanceID)
	if err != nil {
		return vulnerability.NVDWindowCursor{}, err
	}
	return vulnerability.NVDWindowCursor{
		CursorUnixMicro: state.CursorUnixMicro,
		Set:             state.Set,
		HeadUnixMicro:   state.HeadUnixMicro,
		UpdatedAt:       state.UpdatedAt,
	}, nil
}
