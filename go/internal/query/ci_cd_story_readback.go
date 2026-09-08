// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func cicdEvidenceStorySummary(summary map[string]any) string {
	return querycontract.CicdEvidenceStorySummary(summary)
}
