// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decode_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/decode"
)

func TestDerefStringReturnsEmptyForNil(t *testing.T) {
	t.Parallel()
	if got := decode.DerefString(nil); got != "" {
		t.Fatalf("DerefString(nil) = %q, want empty", got)
	}
}

func TestDerefStringReturnsPointedValue(t *testing.T) {
	t.Parallel()
	value := "orders-api"
	if got := decode.DerefString(&value); got != value {
		t.Fatalf("DerefString(&value) = %q, want %q", got, value)
	}
	empty := ""
	if got := decode.DerefString(&empty); got != "" {
		t.Fatalf("DerefString(&empty) = %q, want empty", got)
	}
}

// TestDefaultSchemaMajorVersionIsMajorOne pins the literal every query-layer
// decoder normalizes a version-less row to. The factschema Decode* seams
// dispatch on the major component, so a change here re-routes every legacy row.
func TestDefaultSchemaMajorVersionIsMajorOne(t *testing.T) {
	t.Parallel()
	if decode.DefaultSchemaMajorVersion != "1.0.0" {
		t.Fatalf("DefaultSchemaMajorVersion = %q, want %q", decode.DefaultSchemaMajorVersion, "1.0.0")
	}
}
