// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package deployment

import (
	"fmt"
	"testing"
)

// TestLiveResolveWorkloadSelectorBoundCountsGrantedRowsOnly is the #6801
// review F-R5-1 proof for the deployment-trace selector. The 55 ungranted
// same-name workloads sort before the one granted workload. The scoped name
// read must bound granted rows only. The granted caller resolves its workload
// rather than the typed overflow, and a caller with no grant gets an empty
// result rather than an overflow that signals the crowd exists.
func TestLiveResolveWorkloadSelectorBoundCountsGrantedRowsOnly(t *testing.T) {
	reader, baseCtx := selectorLiveFixture(t)
	const name = "svc-crowded"
	for i := 0; i < 55; i++ {
		reader.write(baseCtx, t, fmt.Sprintf(
			`CREATE (:Workload {id: '%scrowded-%02d', name: '%s', repo_id: '%srepo-b'})`,
			selectorLivePrefix, i, name, selectorLivePrefix))
	}
	reader.write(baseCtx, t, `CREATE (:Workload {id: '`+selectorLivePrefix+`crowded-zz', name: '`+name+`', repo_id: '`+selectorLivePrefix+`repo-a'})`)

	got, err := ResolveWorkloadSelector(selectorScopedContext(baseCtx, selectorLivePrefix+"repo-a"), reader, name, nil, nil)
	if err != nil {
		t.Fatalf("granted caller: ResolveWorkloadSelector() error = %v, want the granted workload", err)
	}
	if got != selectorLivePrefix+"crowded-zz" {
		t.Fatalf("granted caller: ResolveWorkloadSelector() = %q, want %q", got, selectorLivePrefix+"crowded-zz")
	}

	got, err = ResolveWorkloadSelector(selectorScopedContext(baseCtx, selectorLivePrefix+"repo-z"), reader, name, nil, nil)
	if err != nil || got != "" {
		t.Fatalf("no-grant caller: ResolveWorkloadSelector() = %q, %v; want empty, nil", got, err)
	}
}
