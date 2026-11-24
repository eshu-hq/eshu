// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// rependCauseOf is a placeholder the #10972 tests drive red.
func rependCauseOf(reducercontract.Intent) string { return "" }

// passChangeOf is a placeholder the #10972 tests drive red.
func passChangeOf(SupplyChainImpactWriteResult) (string, string) { return "", "" }
