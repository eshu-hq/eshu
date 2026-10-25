// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"fmt"
	"testing"
)

func TestLoadSectionCountsDeclarationsAndNamesTheLowestResolvedMajor(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{sectionRows: []map[string]any{
		{"runtime": "go", "declaration_count": int64(2), "resolved_count": int64(2), "lowest_resolved_major": int64(1)},
		{"runtime": "node", "declaration_count": int64(3), "resolved_count": int64(1), "lowest_resolved_major": int64(18)},
		{"runtime": "ruby", "declaration_count": int64(1), "resolved_count": int64(0), "lowest_resolved_major": nil},
	}}
	section, err := LoadSection(context.Background(), graph, nil, "repository:r_a")
	if err != nil {
		t.Fatalf("LoadSection() error = %v", err)
	}
	if section.Count != 6 || len(section.Runtimes) != 3 || section.Truncated {
		t.Fatalf("section = %+v", section)
	}
	node := section.Runtimes[1]
	if node.Runtime != "node" || node.LowestResolvedMajor == nil || *node.LowestResolvedMajor != 18 ||
		node.DeclarationCount != 3 || node.ResolvedCount != 1 {
		t.Errorf("node summary = %+v", node)
	}
	if ruby := section.Runtimes[2]; ruby.LowestResolvedMajor != nil {
		t.Errorf("a runtime with nothing resolved must report a null lowest major, got %v", *ruby.LowestResolvedMajor)
	}
	call := graph.callContaining(t, "AS lowest_resolved_major")
	if call.params["repo_id"] != "repository:r_a" || call.params["limit"] != maxSectionRuntimes+1 {
		t.Errorf("params = %v", call.params)
	}
}

func TestLoadSectionForARepositoryWithoutDeclarationsIsAnEmptyList(t *testing.T) {
	t.Parallel()

	section, err := LoadSection(context.Background(), &fakeGraph{}, nil, "repository:r_a")
	if err != nil {
		t.Fatalf("LoadSection() error = %v", err)
	}
	if section.Count != 0 || section.Runtimes == nil || len(section.Runtimes) != 0 {
		t.Errorf("section = %+v, want count 0 and a non-nil empty list", section)
	}
}

func TestLoadSectionTruncatesAtTheRuntimeCap(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0, maxSectionRuntimes+1)
	for i := 0; i <= maxSectionRuntimes; i++ {
		rows = append(rows, map[string]any{
			"runtime": fmt.Sprintf("rt%03d", i), "declaration_count": int64(1), "resolved_count": int64(1), "lowest_resolved_major": int64(1),
		})
	}
	section, err := LoadSection(context.Background(), &fakeGraph{sectionRows: rows}, nil, "repository:r_a")
	if err != nil {
		t.Fatalf("LoadSection() error = %v", err)
	}
	if !section.Truncated || len(section.Runtimes) != maxSectionRuntimes || section.Count != maxSectionRuntimes {
		t.Errorf("section truncated=%v runtimes=%d count=%d", section.Truncated, len(section.Runtimes), section.Count)
	}
}

func TestLoadSectionReturnsTheGraphError(t *testing.T) {
	t.Parallel()

	if _, err := LoadSection(context.Background(), &fakeGraph{sectionErr: stubError{}}, nil, "repository:r_a"); err == nil {
		t.Fatal("LoadSection() swallowed the graph error")
	}
}
