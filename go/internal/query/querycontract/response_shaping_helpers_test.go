// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/rowvalue"
)

// TestRowValueForwardersDelegate pins that the five names this package keeps
// after #6597 are pass-throughs and nothing more. The decoder contract itself
// is pinned in the rowvalue package; what has to hold here is that a caller
// naming querycontract.X gets exactly what rowvalue.X returns, including
// StringVal's %v rendering of a present non-string, which is the one case a
// hand-written wrapper could plausibly get wrong.
func TestRowValueForwardersDelegate(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"s":      "eshu",
		"coerce": int64(42),
		"b":      true,
		"i":      int64(7),
		"l":      []any{"a", 1, "b"},
		"f":      float32(0.5),
	}

	for _, key := range []string{"s", "coerce", "missing"} {
		if got, want := StringVal(row, key), rowvalue.StringVal(row, key); got != want {
			t.Errorf("StringVal(%q) = %q, rowvalue.StringVal = %q", key, got, want)
		}
	}
	if got := StringVal(row, "coerce"); got != "42" {
		t.Errorf("StringVal renders a present non-string with %%v: got %q, want %q", got, "42")
	}
	if got, want := BoolVal(row, "b"), rowvalue.BoolVal(row, "b"); got != want {
		t.Errorf("BoolVal = %v, rowvalue.BoolVal = %v", got, want)
	}
	if got, want := IntVal(row, "i"), rowvalue.IntVal(row, "i"); got != want {
		t.Errorf("IntVal = %d, rowvalue.IntVal = %d", got, want)
	}
	if got, want := StringSliceVal(row, "l"), rowvalue.StringSliceVal(row, "l"); !reflect.DeepEqual(got, want) {
		t.Errorf("StringSliceVal = %#v, rowvalue.StringSliceVal = %#v", got, want)
	}
	if got, want := FloatVal(row, "f"), rowvalue.FloatVal(row, "f"); got != want {
		t.Errorf("FloatVal = %v, rowvalue.FloatVal = %v", got, want)
	}
}
