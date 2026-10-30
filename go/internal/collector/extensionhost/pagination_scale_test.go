// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strconv"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestCollectPagesConvergesAtFirstScanScale drives the production NextClaimed
// path through a scripted v1alpha3 satellite at first-scan scale
// (eshu-hq/eshu#9635: 49,047 facts): reassembly must carry every fact
// exactly once with chained cursors, within the default page bound, on one
// claim attempt. It pins the host side of the acceptance bar (a fresh large
// account converges on default caps) before a paging satellite exists.
func TestCollectPagesConvergesAtFirstScanScale(t *testing.T) {
	t.Parallel()

	const factCount = 49047
	const pageFacts = 2500

	item := testWorkItem()
	facts := make([]sdkcollector.Fact, 0, factCount)
	for i := 0; i < factCount; i++ {
		fact := testSDKFact(item)
		fact.StableKey = "scorecard:repo-eshu:" + strconv.Itoa(i)
		fact.SourceRef.FactKey = fact.StableKey
		facts = append(facts, fact)
	}

	var calls []CallResult
	for start := 0; start < len(facts); start += pageFacts {
		end := start + pageFacts
		if end > len(facts) {
			end = len(facts)
		}
		index := len(calls)
		last := end == len(facts)
		page := &ResultPage{PageIndex: index, TotalFacts: factCount}
		if !last {
			page.Cursor = "cursor-" + strconv.Itoa(index+1)
			page.More = true
		}
		calls = append(calls, pagedCall(item, facts[start:end], page))
	}
	if len(calls) > DefaultMaxResultPages {
		t.Fatalf("scripted pages = %d, want <= DefaultMaxResultPages %d", len(calls), DefaultMaxResultPages)
	}

	script := &scriptRunner{calls: calls}
	source := mustNewPagedSource(t, script, 0)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	envelopes := collectFacts(t, collected)
	if len(envelopes) != factCount {
		t.Fatalf("reassembled envelopes = %d, want %d", len(envelopes), factCount)
	}
	seen := make(map[string]int, factCount)
	for _, envelope := range envelopes {
		key := envelope.FactKind + "\x00" + envelope.StableFactKey
		seen[key]++
	}
	for i := 0; i < factCount; i++ {
		key := "dev.eshu.examples.scorecard.check\x00scorecard:repo-eshu:" + strconv.Itoa(i)
		if seen[key] != 1 {
			t.Fatalf("fact %q seen %d times, want exactly once", key, seen[key])
		}
	}

	if len(script.requests) != len(calls) {
		t.Fatalf("runner calls = %d, want %d", len(script.requests), len(calls))
	}
	if script.requests[0].Continuation != nil {
		t.Fatalf("first request continuation = %+v, want nil", script.requests[0].Continuation)
	}
	for i := 1; i < len(script.requests); i++ {
		want := "cursor-" + strconv.Itoa(i)
		got := script.requests[i].Continuation
		if got == nil || got.Cursor != want {
			t.Fatalf("request %d continuation = %+v, want cursor %q", i, got, want)
		}
	}
}
