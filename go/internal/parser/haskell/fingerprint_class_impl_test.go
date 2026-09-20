// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package haskell

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseFingerprintsHaskellClassDefaultImpl pins the class-body collision:
// handleSignature pre-creates the function row (no-body skip) under the same
// key the later default-implementation binding uses, so the binding must
// attach its real body instead of leaving the row without fingerprint keys.
func TestParseFingerprintsHaskellClassDefaultImpl(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Svc.hs", `module Svc where

class Service a where
  compute :: a -> Int -> Int
  compute seed limit =
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
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	item := assertBucketName(t, payload, "functions", "compute")
	exact, ok := item[fingerprint.KeyExact].(string)
	if !ok || exact == "" {
		t.Fatalf("class default impl body_fp_exact = %#v, want non-empty exact fingerprint", item[fingerprint.KeyExact])
	}
	count, ok := item[fingerprint.KeyTokenCount].(int)
	if !ok || count < fingerprint.MinTokenCount {
		t.Fatalf("class default impl body_token_count = %#v, want >= %d", item[fingerprint.KeyTokenCount], fingerprint.MinTokenCount)
	}
}
