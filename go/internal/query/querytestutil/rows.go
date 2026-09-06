// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RowIDs plucks the "id" value of every row. It moved here from the impact
// change-surface tests with lane B2 of #6060 because the root live-backend
// proof test pins the same row identity and test files cannot share helpers
// across packages.
func RowIDs(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, querycontract.StringVal(row, "id"))
	}
	return ids
}
