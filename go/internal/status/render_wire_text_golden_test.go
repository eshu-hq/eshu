// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "testing"

const renderTextGoldenPath = "testdata/render_text_golden.txt"

// TestRenderWireText_ByteForByteGolden locks RenderText's output against a
// committed golden fixture byte-for-byte, the text-surface twin of
// TestRenderWireJSON_ByteForByteGolden (render_wire_golden_test.go). Both
// pin RenderText/RenderJSON before the #6775 package nest so a rendering
// regression during the split is caught here instead of by an operator.
func TestRenderWireText_ByteForByteGolden(t *testing.T) {
	t.Parallel()

	report := BuildReport(maxRawSnapshot(), DefaultOptions())
	got := []byte(RenderText(report))

	assertGoldenBytes(t, renderTextGoldenPath, got)
}
