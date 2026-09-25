// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestVersionCountsByPackageIDZeroFillsAndSkipsEmptyPage proves the exported
// count read keeps the contract attachPackageVersionCounts relies on: an empty
// page makes no graph call, and a uid absent from the result reads as zero.
func TestVersionCountsByPackageIDZeroFillsAndSkipsEmptyPage(t *testing.T) {
	calls := 0
	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		calls++
		if ids, _ := params["package_ids"].([]string); len(ids) != 2 {
			t.Fatalf("package_ids = %#v, want the two requested uids", params["package_ids"])
		}
		return []map[string]any{{"package_id": "pkg-a", "version_count": int64(2)}}, nil
	}}

	counts, err := VersionCountsByPackageID(context.Background(), reader, nil)
	if err != nil || len(counts) != 0 || calls != 0 {
		t.Fatalf("empty page: counts=%v err=%v calls=%d, want empty map, nil, 0 calls", counts, err, calls)
	}
	counts, err = VersionCountsByPackageID(context.Background(), reader, []string{"pkg-a", "pkg-zero"})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want nil and exactly one call", err, calls)
	}
	if counts["pkg-a"] != 2 || counts["pkg-zero"] != 0 {
		t.Fatalf("counts = %v, want pkg-a=2 and pkg-zero=0", counts)
	}
}
