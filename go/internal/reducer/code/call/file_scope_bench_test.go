// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// fileScopeBenchEnvelopes is one delta-generation repository with
// deltaPaths changed files plus one full-generation repository with
// fullFiles file facts, the two shapes BuildFileScopesByRepoID scopes.
func fileScopeBenchEnvelopes(deltaPaths, fullFiles int) []facts.Envelope {
	changed := make([]string, 0, deltaPaths)
	for i := 0; i < deltaPaths; i++ {
		changed = append(changed, fmt.Sprintf("src/pkg%03d/file%04d.go", i%100, i))
	}
	envelopes := make([]facts.Envelope, 0, fullFiles+2)
	envelopes = append(envelopes,
		facts.Envelope{FactKind: factload.FactKindRepository, Payload: map[string]any{
			"repo_id": "repo-delta", "path": "/repos/delta", "delta_generation": true,
			"delta_relative_paths": changed,
		}},
		facts.Envelope{FactKind: factload.FactKindRepository, Payload: map[string]any{
			"repo_id": "repo-full", "path": "/repos/full",
		}},
	)
	for i := 0; i < fullFiles; i++ {
		relativePath := fmt.Sprintf("lib/mod%03d/file%05d.py", i%100, i)
		envelopes = append(envelopes, facts.Envelope{FactKind: factload.FactKindFile, Payload: map[string]any{
			"repo_id": "repo-full", "relative_path": relativePath,
			"parsed_file_data": map[string]any{"path": relativePath},
		}})
	}
	return envelopes
}

// BenchmarkBuildFileScopesByRepoID measures the per-materialization file
// scope build the #10023 change touches (delta collection now also keeps the
// delta repository set and unusable reasons).
func BenchmarkBuildFileScopesByRepoID(b *testing.B) {
	envelopes := fileScopeBenchEnvelopes(2000, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := BuildFileScopesByRepoID(envelopes)
		if len(result.ScopesByRepoID) != 2 {
			b.Fatalf("scopes = %d, want 2", len(result.ScopesByRepoID))
		}
	}
}
