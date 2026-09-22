// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"path/filepath"
	"testing"
)

// TestRenderEmptySnapshotGolden locks the wire output for a snapshot with
// nothing in it. The populated goldens in render_wire_golden_test.go cannot
// catch a nil-versus-empty-slice regression, because every section they
// exercise has rows: a projector that starts returning nil where it used to
// return an empty slice flips a section from [] to null in the operator JSON,
// and only an empty snapshot shows it.
//
// It is also the case an operator actually hits first -- a freshly started
// stack with no facts yet -- so a section rendering null there is the shape a
// dashboard breaks on.
func TestRenderEmptySnapshotGolden(t *testing.T) {
	t.Parallel()

	report := BuildReport(RawSnapshot{}, DefaultOptions())

	encoded, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	assertGoldenBytes(t, filepath.Join("testdata", "render_empty_json_golden.json"), encoded)
	assertGoldenBytes(t, filepath.Join("testdata", "render_empty_text_golden.txt"),
		[]byte(RenderText(report)))
}
