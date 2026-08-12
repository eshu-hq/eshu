// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"slices"
	"testing"
)

// BenchmarkCanonicalizeRepoDependencyBackfillProofTwoCalls preserves the exact
// #5975 input: two independently constructed retained-shape Odù values, with
// the second fact order reversed, followed by two canonicalizations.
func BenchmarkCanonicalizeRepoDependencyBackfillProofTwoCalls(b *testing.B) {
	ctx := context.Background()
	first := RepoDependencyBackfillProofOdu()
	second := RepoDependencyBackfillProofOdu()
	slices.Reverse(second.Facts)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(len(first.Facts)+len(second.Facts)), "facts/op")
	for range b.N {
		if _, err := CanonicalizeOdu(ctx, first, nil); err != nil {
			b.Fatal(err)
		}
		if _, err := CanonicalizeOdu(ctx, second, nil); err != nil {
			b.Fatal(err)
		}
	}
}
