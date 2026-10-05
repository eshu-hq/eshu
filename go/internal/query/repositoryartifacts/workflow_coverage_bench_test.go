// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"testing"
)

func BenchmarkWorkflowCoverageStory(b *testing.B) {
	for _, tc := range []struct {
		name            string
		total, workflow int
	}{
		{"uncapped_empty", 4999, 0},
		{"capped_empty", 5000, 0},
		{"capped_present", 5000, 5000},
	} {
		files := coverageFiles(tc.total, tc.workflow)
		store := &orderedCoverageStore{}
		correlations := &emptyCoverageCorrelations{}
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				_, err := LoadRepositoryScopedCICDEvidenceFromFiles(context.Background(), store, correlations, coverageRepoID, files)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
