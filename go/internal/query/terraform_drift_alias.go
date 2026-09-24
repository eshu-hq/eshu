// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the Terraform config-vs-state drift aliases must live in package query so the APIRouter field and the cmd/api and cmd/mcp-server wiring compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/terraform/drift"
)

// terraform_drift_alias.go is the root alias shim for the Terraform
// config-vs-state drift family, which moved to internal/query/terraform/drift
// (#6642). cmd/api and cmd/mcp-server build the handler and its store through
// package query, so these stay until the #6642 alias sweep.

// TerraformConfigStateDriftHandler serves the Terraform config-vs-state drift
// findings route. See drift.Handler.
type TerraformConfigStateDriftHandler = drift.Handler

// NewPostgresTerraformConfigStateDriftFindingStore forwards unchanged to
// drift.NewPostgresFindingStore.
func NewPostgresTerraformConfigStateDriftFindingStore(db *sql.DB) *drift.PostgresFindingStore {
	return drift.NewPostgresFindingStore(db)
}
