// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// TestDecodeParsedFileDataTolerantSlice_LogsSkippedElements is the operator
// signal regression lock for issue #5445 finding 2.
// decodeParsedFileDataTolerantSlice silently skips a malformed element and
// returns (out, true) even when EVERY element was malformed -- a len-0
// result indistinguishable from "this file legitimately has no rows for
// this bucket." A producer regression that starts emitting a wrong element
// shape for one bucket would silently degrade that bucket's evidence to
// zero with no operator-visible signal. This test asserts a warn-level log
// records the skipped-element count, closing that absence-of-data-vs
// -absence-of-evaluation gap.
//
// This test swaps the process-wide slog default logger, so it sets the
// default logger for the duration of the call and restores it before
// returning, and relies on Go's sequential-then-parallel test ordering (all
// non-parallel top-level tests in this package complete before any parked
// t.Parallel() subtest runs) to avoid racing with the slog default other
// tests in this package might read.
func TestDecodeParsedFileDataTolerantSlice_LogsSkippedElements(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	// LevelInfo, deliberately: the point of this record is that an operator
	// running default settings sees a degraded bucket. A LevelDebug handler
	// would capture the record whatever level it was emitted at, so it could
	// not fail if someone lowered it back to Debug and made it invisible in
	// production.
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	rows, ok := decodeParsedFileDataTolerantSlice[codegraphv1.TerraformModule]([]any{
		"not-an-object",
		map[string]any{"name": "vpc", "source": "terraform-aws-modules/vpc/aws"},
		map[string]any{"name": 42}, // wrong-typed Name field: decodeMapInto errors, row skipped
	})
	if !ok {
		t.Fatal("ok = false, want true (top-level shape is a recognized slice)")
	}
	if len(rows) != 1 || rows[0].Name != "vpc" {
		t.Fatalf("rows = %#v, want one vpc row", rows)
	}

	logged := buf.String()
	if !strings.Contains(logged, "skipped_elements=2") {
		t.Fatalf("log output = %q, want it to record skipped_elements=2", logged)
	}
	if !strings.Contains(logged, "total_elements=3") {
		t.Fatalf("log output = %q, want it to record total_elements=3", logged)
	}
	if !strings.Contains(logged, "TerraformModule") {
		t.Fatalf("log output = %q, want it to name the decoded element type", logged)
	}
}

// TestDecodeParsedFileDataTolerantSlice_NoLogWhenNothingSkipped proves the
// skipped-element record stays silent on the common well-formed-input path, so
// it never adds noise to a healthy decode. This one keeps a LevelDebug handler
// on purpose: proving silence at the most permissive level is stronger than
// proving it at the level the record is actually emitted on.
func TestDecodeParsedFileDataTolerantSlice_NoLogWhenNothingSkipped(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	rows, ok := decodeParsedFileDataTolerantSlice[codegraphv1.TerraformModule]([]any{
		map[string]any{"name": "vpc", "source": "terraform-aws-modules/vpc/aws"},
	})
	if !ok || len(rows) != 1 {
		t.Fatalf("rows, ok = %#v, %v, want one row and ok=true", rows, ok)
	}
	if buf.Len() != 0 {
		t.Fatalf("log output = %q, want no log emitted when nothing was skipped", buf.String())
	}
}
