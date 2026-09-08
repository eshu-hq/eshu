// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
)

// attachSemanticSummary forwards to entitysemantics.AttachSemanticSummary.
// The implementation moved to querycontract for #6060 and on to entitysemantics
// for #6060 lane A L3; this wrapper keeps root callers unchanged.
func attachSemanticSummary(result map[string]any) {
	entitysemantics.AttachSemanticSummary(result)
}
