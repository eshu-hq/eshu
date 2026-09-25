// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package haskell

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// multiEquationFirstBody is a two-equation function whose first equation is
// large enough to clear the fingerprint floor on its own, so the row carries
// fingerprint keys before and after the fix. The second equation is the only
// text that differs between the two variants under test.
const multiEquationFirstBody = `module MultiEq where

combine :: Int -> Int -> Int
combine seed limit =
  let first = seed + limit
      second = first * 2
      third = second - seed
      fourth = third + first
      fifth = fourth * second
      sixth = fifth - third
      seventh = sixth + fourth
      eighth = seventh * fifth
      ninth = eighth - sixth
      total = ninth + seventh + eighth
  in total
`

// TestParseFingerprintCoversEveryEquation is the issue #6865 regression: an
// edit confined to the second pattern-matching clause of a multi-equation
// function must change its fingerprint. Before the fix only the first
// equation fed the fingerprint, so both variants hashed identically.
func TestParseFingerprintCoversEveryEquation(t *testing.T) {
	t.Parallel()

	first := multiEquationFirstBody + "combine seed 0 = seed\n"
	second := multiEquationFirstBody + "combine seed 0 = seed + 1\n"

	firstPath := writeSource(t, "MultiEqFirst.hs", first)
	firstPayload, err := Parse(firstPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	firstItem := assertBucketName(t, firstPayload, "functions", "combine")
	firstExact, ok := firstItem[fingerprint.KeyExact].(string)
	if !ok || firstExact == "" {
		t.Fatalf("first variant body_fp_exact = %#v, want non-empty exact fingerprint", firstItem[fingerprint.KeyExact])
	}

	secondPath := writeSource(t, "MultiEqSecond.hs", second)
	secondPayload, err := Parse(secondPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	secondItem := assertBucketName(t, secondPayload, "functions", "combine")
	secondExact, ok := secondItem[fingerprint.KeyExact].(string)
	if !ok || secondExact == "" {
		t.Fatalf("second variant body_fp_exact = %#v, want non-empty exact fingerprint", secondItem[fingerprint.KeyExact])
	}

	if firstExact == secondExact {
		t.Fatalf("body_fp_exact unchanged by second-equation edit: %q", firstExact)
	}

	// The token count must span both equations, proving the hash covers the
	// concatenation rather than either equation alone.
	singlePath := writeSource(t, "MultiEqFirstOnly.hs", multiEquationFirstBody)
	singlePayload, err := Parse(singlePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	singleItem := assertBucketName(t, singlePayload, "functions", "combine")
	singleCount, ok := singleItem[fingerprint.KeyTokenCount].(int)
	if !ok {
		t.Fatalf("single-equation body_token_count = %#v, want int", singleItem[fingerprint.KeyTokenCount])
	}
	firstCount, ok := firstItem[fingerprint.KeyTokenCount].(int)
	if !ok {
		t.Fatalf("first variant body_token_count = %#v, want int", firstItem[fingerprint.KeyTokenCount])
	}
	if firstCount <= singleCount {
		t.Fatalf("two-equation body_token_count = %d, want > single-equation %d", firstCount, singleCount)
	}
}
