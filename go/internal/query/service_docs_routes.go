// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// extractDocsRoutes returns the sorted, de-duplicated docs-like route
// references quoted in content. The implementation moved to querycontract for
// #6060; this wrapper keeps root callers unchanged.
func extractDocsRoutes(content string) []string {
	return querycontract.ExtractDocsRoutes(content)
}
