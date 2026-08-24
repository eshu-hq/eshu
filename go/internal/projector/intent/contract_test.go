// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intent

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope facts.Envelope
		want     string
	}{
		{
			name: "source ref wins",
			envelope: facts.Envelope{
				SourceRef:     facts.Ref{SourceSystem: " azure-resource-graph "},
				CollectorKind: "azure-collector",
			},
			want: "azure-resource-graph",
		},
		{
			name: "blank source ref falls back",
			envelope: facts.Envelope{
				SourceRef:     facts.Ref{SourceSystem: "  "},
				CollectorKind: " azure-collector ",
			},
			want: "azure-collector",
		},
		{
			name:     "both empty",
			envelope: facts.Envelope{},
			want:     "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SourceSystem(test.envelope); got != test.want {
				t.Fatalf("SourceSystem() = %q, want %q", got, test.want)
			}
		})
	}
}
