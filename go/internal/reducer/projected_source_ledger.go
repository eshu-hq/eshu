// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// This file is the transitional compatibility surface for the projected-
// source ledger contract that moved to [reducercontract] and [payloadcore]
// (issue #6061). Reducer-root call sites (AWS, Azure, GCP, and security-group
// edge writers) keep their current spelling; this entry is deleted once its
// last reducer-root caller has moved into a family subpackage.

// ProjectedSourceLedger records and enumerates the source-node uids of
// projected CloudResource edges. See [reducercontract.ProjectedSourceLedger]
// for the full contract.
type ProjectedSourceLedger = reducercontract.ProjectedSourceLedger

// sourceUIDsFromRowsByKey extracts distinct string values stored under key
// from a batch of edge rows. See [payloadcore.SourceUIDsFromRowsByKey].
func sourceUIDsFromRowsByKey(rows []map[string]any, key string) []string {
	return payloadcore.SourceUIDsFromRowsByKey(rows, key)
}
