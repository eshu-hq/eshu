// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"

// javaScriptExpressServerSymbols mirrors the parent parser package's helper of
// the same name so javascript_dead_code_roots_test.go keeps its original call
// shape after relocation. It is a thin indirection over the production
// jsparser.ExpressServerSymbols, kept as its own file to match the exact
// move-only diff (issue #6062, following the Elixir precedent in #6335).
func javaScriptExpressServerSymbols(express map[string]any) []string {
	return jsparser.ExpressServerSymbols(express)
}
