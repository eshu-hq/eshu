// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import "testing"

func TestGoNormalizeTypeNamePreservesArrayAndSliceElementNames(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"[]Worker":              "Worker",
		"[4]Worker":             "Worker",
		"[]pkg.Worker":          "Worker",
		"[]*pkg.Worker[T]":      "Worker",
		"pkg.Worker[T]":         "Worker",
		"*pkg.Worker[K, V]":     "Worker",
		"map[string]Worker":     "map",
		"chan<- *pkg.Worker[T]": "Worker",
	}
	for input, want := range cases {
		input := input
		want := want
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeTypeName(input); got != want {
				t.Fatalf("NormalizeTypeName(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
