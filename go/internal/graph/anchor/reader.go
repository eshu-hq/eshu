// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"context"
	"errors"
	"fmt"
)

// RowReader is the single-row graph read port the census needs. The reducer's
// query.GraphQuery satisfies it.
type RowReader interface {
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
}

// ReaderCensus runs [CensusCypher] over a [RowReader].
type ReaderCensus struct {
	// Reader is the graph read port.
	Reader RowReader
}

// AnchorCensus implements [CensusSource].
func (r ReaderCensus) AnchorCensus(ctx context.Context) (Census, error) {
	row, err := r.Reader.RunSingle(ctx, CensusCypher, CensusParameters())
	if err != nil {
		return Census{}, fmt.Errorf("run anchor census: %w", err)
	}
	if row == nil {
		return Census{}, errors.New("anchor census returned no row")
	}
	return ParseCensusRow(row)
}
