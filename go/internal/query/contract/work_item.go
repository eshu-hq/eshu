// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "github.com/eshu-hq/eshu/go/internal/query/workitem"

// Declared by the work-item family (workitem/capability.go, #6060), not copied
// here. See the semanticsearch entry in contract_capability_matrix.go for why.
func init() {
	register(workItemEvidenceCapability, workitem.EvidenceSupport())
}
