// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"testing"
)

func TestSemanticEntityWriterConstructorsSetExclusiveWriteModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  *SemanticEntityWriter
		want semanticEntityWriteMode
	}{
		{
			name: "legacy row templates",
			got:  NewSemanticEntityWriter(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeLegacyRows,
		},
		{
			name: "single-row parameterized properties",
			got:  NewSemanticEntityWriterWithParameterizedRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeParameterizedRows,
		},
		{
			name: "batched property maps",
			got:  NewSemanticEntityWriterWithBatchedProperties(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeBatchedProperties,
		},
		{
			name: "merge-first rows",
			got:  NewSemanticEntityWriterWithMergeFirstRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeMergeFirstRows,
		},
		{
			name: "canonical node rows",
			got:  NewSemanticEntityWriterWithCanonicalNodeRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeCanonicalNodeRows,
		},
	}

	for _, tt := range tests {
		if tt.got.writeMode != tt.want {
			t.Fatalf("%s writeMode = %v, want %v", tt.name, tt.got.writeMode, tt.want)
		}
	}
}
