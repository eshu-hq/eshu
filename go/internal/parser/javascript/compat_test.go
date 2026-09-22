// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import "github.com/eshu-hq/eshu/go/internal/parser/javascript/deadcode"

// javaScriptExpressServerSymbols mirrors the dead-code helper of the same name
// so dead_code_roots_test.go keeps its original call shape after relocation. It
// is a thin indirection over the production deadcode.ExpressServerSymbols, kept
// as its own file to match the exact move-only diff (issue #6062, following the
// Elixir precedent in #6335). The target moved from the parent javascript
// package into deadcode/ with the root checks it belongs to (issue #6771); the
// call shape here is unchanged.
func javaScriptExpressServerSymbols(express map[string]any) []string {
	return deadcode.ExpressServerSymbols(express)
}
