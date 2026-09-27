// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"
)

// TestValidateSourceCoverageRejectsFanOutMultiplierOutsideBoundedKeyBatch
// proves fan_out_multiplier is scoped to keyed_support bounded_key_batch
// rows only (#6590): a non-zero value on any other class, or on a
// keyed_support row with key_bound single_key, is rejected before any
// arithmetic check runs.
func TestValidateSourceCoverageRejectsFanOutMultiplierOutsideBoundedKeyBatch(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name        string
		disposition NonHotDisposition
	}{
		{
			name: "degree_bounded class",
			disposition: NonHotDisposition{
				Class:            NonHotClassDegreeBounded,
				SourceDigest:     digest,
				KeyBound:         NonHotKeyBoundSingle,
				MaxDegree:        nonHotCorpusMaxCALLSDegree,
				FanOutMultiplier: 1,
			},
		},
		{
			name: "keyed_support single_key",
			disposition: NonHotDisposition{
				Class:            NonHotClassKeyedSupport,
				SourceDigest:     digest,
				KeyBound:         NonHotKeyBoundSingle,
				MaxResults:       10,
				FanOutMultiplier: 1,
			},
		},
		{
			name: "label_inventory class",
			disposition: NonHotDisposition{
				Class:            NonHotClassLabelInventory,
				SourceDigest:     digest,
				Label:            "Repository",
				MaxResults:       10,
				FanOutMultiplier: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateNonHotDisposition("key", test.disposition)
			if !containsSubstring(got, "fan_out_multiplier only applies to keyed_support bounded_key_batch rows") {
				t.Fatalf("validateNonHotDisposition() = %v, want fan_out_multiplier scope violation", got)
			}
		})
	}
}

// TestValidateSourceCoverageRejectsFanOutMultiplierBelowOne proves a
// declared fan_out_multiplier under 1 is rejected: a per-key row multiplier
// can never be zero or negative.
func TestValidateSourceCoverageRejectsFanOutMultiplierBelowOne(t *testing.T) {
	digest := strings.Repeat("a", 64)
	disposition := NonHotDisposition{
		Class:            NonHotClassKeyedSupport,
		SourceDigest:     digest,
		KeyBound:         NonHotKeyBoundBatch,
		MaxKeys:          10,
		MaxResults:       0,
		FanOutMultiplier: -1,
	}
	got := validateNonHotDisposition("key", disposition)
	if !containsSubstring(got, "fan_out_multiplier") || !containsSubstring(got, ">= 1") {
		t.Fatalf("validateNonHotDisposition() = %v, want fan_out_multiplier >= 1 violation", got)
	}
}

// TestValidateSourceCoverageRejectsFanOutMultiplierMismatch proves
// max_results must equal max_keys x fan_out_multiplier exactly -- it is a
// derived value, not one picked independently of the declared multiplier.
func TestValidateSourceCoverageRejectsFanOutMultiplierMismatch(t *testing.T) {
	digest := strings.Repeat("a", 64)
	disposition := NonHotDisposition{
		Class:            NonHotClassKeyedSupport,
		SourceDigest:     digest,
		KeyBound:         NonHotKeyBoundBatch,
		MaxKeys:          250,
		MaxResults:       751,
		FanOutMultiplier: 3,
	}
	got := validateNonHotDisposition("key", disposition)
	want := "bounded_key_batch requires max_results == max_keys x fan_out_multiplier (250 x 3 = 750, got 751); max_results is derived, not picked"
	if !containsSubstring(got, want) {
		t.Fatalf("validateNonHotDisposition() = %v, want %q", got, want)
	}
}

// TestValidateSourceCoverageAcceptsFanOutMultiplier proves a correctly
// derived max_results (max_keys x fan_out_multiplier, exact equality) on a
// keyed_support bounded_key_batch row passes with no violation.
func TestValidateSourceCoverageAcceptsFanOutMultiplier(t *testing.T) {
	digest := strings.Repeat("a", 64)
	disposition := NonHotDisposition{
		Class:            NonHotClassKeyedSupport,
		SourceDigest:     digest,
		KeyBound:         NonHotKeyBoundBatch,
		MaxKeys:          250,
		MaxResults:       250,
		FanOutMultiplier: 1,
	}
	if got := validateNonHotDisposition("key", disposition); len(got) != 0 {
		t.Fatalf("validateNonHotDisposition() = %v, want no violations", got)
	}
}

// #7325's outlier CALLS-fanout row (runOutlierGraphRows) carries a literal
// max_results (281250 = 250 keys x the #6649 corpus CALLS-degree floor
// 1125), not a fan_out_multiplier: nothing in code enforces that every key
// produces exactly 1125 rows -- 1125 is a measured corpus maximum, not a
// code-enforced per-key count (see FanOutMultiplier's doc comment in
// source_coverage.go, which requires a named enforcer -- a uniqueness
// constraint, writer guard, or schema-fixed label list -- before the field
// applies). The only validator rule that covers a plain keyed_support
// literal max_results is "requires max_results" (must be > 0,
// TestValidateSourceCoverageRejectsIncompleteTypedEvidence), which is
// already covered generically there and is not specific to this row's
// value; no rule checks a literal max_results against its derivation
// comment. A seeded RED/GREEN pinned to the 250 x 1125 = 281250 arithmetic
// was removed for that reason rather than kept against a rule that does not
// actually apply to this row.

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
