// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 authz seam shim for #6060: the exported RepositoryAccessFilter spelling is named by root call sites while the type itself lives in querycontract, so this file must stay in package query.

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryAccessFilter is the root spelling of the querycontract seam type.
// Exported impact-seam forwarders name it in their signatures so the impact
// family can move without touching callers. See #6060.
type RepositoryAccessFilter = querycontract.RepositoryAccessFilter
