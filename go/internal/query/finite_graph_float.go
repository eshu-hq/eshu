// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// finiteGraphFloat reads key from row as a float64 and rejects NaN/Inf. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func finiteGraphFloat(row map[string]any, key, subject string) (float64, error) {
	return querycontract.FiniteGraphFloat(row, key, subject)
}
