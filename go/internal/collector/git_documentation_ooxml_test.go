// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"
)

func TestOOXMLPreflightBlocksExtractionOnlyForFatalWarnings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input ooxmlpreflight.Result
		want  bool
	}{
		{
			name: "hidden and annotation warnings stay extractable",
			input: ooxmlpreflight.Result{Warnings: []ooxmlpreflight.Warning{
				{Class: ooxmlpreflight.WarningAnnotationTextSkipped, Count: 1},
				{Class: ooxmlpreflight.WarningHiddenContentSkipped, Count: 1},
			}},
			want: false,
		},
		{
			name: "external relationships block extraction",
			input: ooxmlpreflight.Result{Warnings: []ooxmlpreflight.Warning{
				{Class: ooxmlpreflight.WarningExternalRelationship, Count: 1},
			}},
			want: true,
		},
		{
			name: "resource limits block extraction",
			input: ooxmlpreflight.Result{Warnings: []ooxmlpreflight.Warning{
				{Class: ooxmlpreflight.WarningResourceLimitExceeded, Count: 1},
			}},
			want: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ooxmlPreflightBlocksExtraction(tc.input); got != tc.want {
				t.Fatalf("ooxmlPreflightBlocksExtraction() = %v, want %v", got, tc.want)
			}
		})
	}
}
