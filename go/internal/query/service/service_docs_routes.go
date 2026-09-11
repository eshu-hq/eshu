// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"github.com/eshu-hq/eshu/go/internal/query/service/evidence"
)

// extractDocsRoutes returns the sorted, de-duplicated docs-like route
// references quoted in content. The implementation moved to the
// service/evidence leaf for #6060; this wrapper keeps root callers unchanged.
func extractDocsRoutes(content string) []string {
	return evidence.ExtractDocsRoutes(content)
}
