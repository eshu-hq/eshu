// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"fmt"
	"testing"
)

// BenchmarkOverlayDeltaBaseUnion measures the #7797 overlay on a union-sized
// input (#8780 no-regression): a 2,000-file base with a file, content, and
// content_entity row per file plus one observability row on every tenth file,
// overlaid by a 200-file cumulative delta that changes 150 files and deletes
// 50. It exercises the file-attributable kind lookup on every base row.
func BenchmarkOverlayDeltaBaseUnion(b *testing.B) {
	base := []Envelope{deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil))}
	for i := range 2000 {
		rel := fmt.Sprintf("pkg/f%04d.go", i)
		base = append(base,
			deltaUnionBaseEnvelope("file", "file:"+rel, "bf"+rel, map[string]any{"relative_path": rel}),
			deltaUnionBaseEnvelope("content", "content:"+rel, "bc"+rel, map[string]any{"content_path": rel}),
			deltaUnionBaseEnvelope("content_entity", "entity:"+rel, "be"+rel, map[string]any{"relative_path": rel}),
		)
		if i%10 == 0 {
			base = append(base, deltaUnionBaseEnvelope(ObservabilitySourceInstanceFactKind, "obs:"+rel+":gen-base-1", "bo"+rel,
				map[string]any{"relative_path": rel}))
		}
	}
	changed := make([]string, 0, 150)
	deleted := make([]string, 0, 50)
	delta := []Envelope{}
	for i := range 200 {
		rel := fmt.Sprintf("pkg/f%04d.go", i*10)
		if i < 150 {
			changed = append(changed, rel)
			delta = append(delta,
				deltaUnionTestEnvelope("file", "file:"+rel, "df"+rel, map[string]any{"relative_path": rel}),
				deltaUnionTestEnvelope("content", "content:"+rel, "dc"+rel, map[string]any{"content_path": rel}),
			)
			continue
		}
		deleted = append(deleted, rel)
	}
	delta = append(delta, deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, changed, deleted)))
	scope := ParseDeltaScope(delta)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = OverlayDeltaBase(base, delta, scope)
	}
}
