// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scip

import (
	"regexp"
	"strings"
	"testing"
)

// nameFromSymbolOldCompilePerCall reproduces nameFromSymbol's
// pre-hoist behavior (issue #4874): it compiled the `[/#]` separator regex
// inside the function body on every call instead of reusing a package-level
// *regexp.Regexp. It exists only as the "OLD" side of the
// BenchmarkScipNameFromSymbolRegexHoist before/after comparison; production
// code always calls the hoisted nameFromSymbol.
func nameFromSymbolOldCompilePerCall(symbol string) string {
	stripped := strings.TrimRight(symbol, ".#")
	stripped = trailingCallRe.ReplaceAllString(stripped, "")
	parts := regexp.MustCompile(`[/#]`).Split(stripped, -1)
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return symbol
	}
	return parts[len(parts)-1]
}

// BenchmarkScipNameFromSymbolRegexHoist is the Prove-The-Theory-First /
// structural-certain evidence for issue #4874's regex hoist: it measures the
// pre-hoist per-call regexp.MustCompile behavior against the hoisted
// package-level separatorRe on the same symbol shape with -benchmem.
func BenchmarkScipNameFromSymbolRegexHoist(b *testing.B) {
	symbol := "scip-python python . . `pkg/sub/module`/Class#method()."

	b.Run("Old_CompilePerCall", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = nameFromSymbolOldCompilePerCall(symbol)
		}
	})

	b.Run("New_HoistedPackageVar", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = nameFromSymbol(symbol)
		}
	})
}
