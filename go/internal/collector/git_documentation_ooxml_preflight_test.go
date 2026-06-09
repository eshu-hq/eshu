package collector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"
)

// TestOOXMLPreflightBlockedTreatsContentSkippedWarningsAsNonBlocking is the
// regression guard for the office-document extraction gate: the ooxmlpreflight
// package sets Result.Safe=false for ANY warning, including the benign
// hidden-content and skipped-annotation notes, so gating extraction on Safe
// dropped every workbook/deck/document that merely contained a hidden sheet,
// hidden slide, or comment. Only genuine safety, resource, or structural
// warnings must block extraction.
func TestOOXMLPreflightBlockedTreatsContentSkippedWarningsAsNonBlocking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		warnings []ooxmlpreflight.Warning
		want     bool
	}{
		{name: "no warnings", warnings: nil, want: false},
		{
			name:     "hidden content skipped is benign",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningHiddenContentSkipped, Count: 1}},
			want:     false,
		},
		{
			name:     "annotation text skipped is benign",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningAnnotationTextSkipped, Count: 2}},
			want:     false,
		},
		{
			name: "both benign content warnings",
			warnings: []ooxmlpreflight.Warning{
				{Class: ooxmlpreflight.WarningHiddenContentSkipped, Count: 1},
				{Class: ooxmlpreflight.WarningAnnotationTextSkipped, Count: 1},
			},
			want: false,
		},
		{
			name:     "resource limit exceeded blocks",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningResourceLimitExceeded, Count: 1}},
			want:     true,
		},
		{
			name:     "malformed container blocks",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningMalformedContainer, Count: 1}},
			want:     true,
		},
		{
			name:     "external relationship blocks",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningExternalRelationship, Count: 1}},
			want:     true,
		},
		{
			name:     "macro enabled blocks",
			warnings: []ooxmlpreflight.Warning{{Class: ooxmlpreflight.WarningUnsupportedMacroEnabled, Count: 1}},
			want:     true,
		},
		{
			name: "benign mixed with blocking still blocks",
			warnings: []ooxmlpreflight.Warning{
				{Class: ooxmlpreflight.WarningHiddenContentSkipped, Count: 1},
				{Class: ooxmlpreflight.WarningResourceLimitExceeded, Count: 1},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ooxmlpreflight.Result{Safe: len(tt.warnings) == 0, Warnings: tt.warnings}
			if got := ooxmlPreflightBlocked(result); got != tt.want {
				t.Fatalf("ooxmlPreflightBlocked(%v) = %v, want %v", tt.warnings, got, tt.want)
			}
		})
	}
}
