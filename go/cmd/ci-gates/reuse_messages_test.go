// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const targetMessageMergeSHA = "9999999999999999999999999999999999999999"

func messageCompareJSON(merge, head, message string) string {
	commits := []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}{
		{SHA: head, Commit: struct {
			Message string `json:"message"`
		}{Message: message}},
		{SHA: merge, Commit: struct {
			Message string `json:"message"`
		}{Message: "Merge " + head + " into " + testedBaseSHA}},
	}
	encoded, _ := json.Marshal(struct {
		TotalCommits int `json:"total_commits"`
		Commits      any `json:"commits"`
	}{TotalCommits: len(commits), Commits: commits})
	return string(encoded)
}

func TestProbeReuseRefusesChangedStoredMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
	}{
		{name: "attribution", message: "Co-" + "authored-by: Codex <bot@example.com>"},
		{name: "private identifier", message: "bg-" + "prod"},
		{name: "growth trailer", message: "Dirgate-Growth: cmd/ci-gates +1 #8847 pending split"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := append([]mappedGHRoute{
				{match: "compare/" + testedBaseSHA + "..." + sourceMergeSHA, output: messageCompareJSON(sourceMergeSHA, reuseOldSHA, "safe")},
				{match: "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA, output: messageCompareJSON(targetMessageMergeSHA, reuseHeadSHA, tc.message)},
			}, boundFullSourceRoutes(boundFullStatus())...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
			got, err := probeReuse(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if err != nil || got.Reuse {
				t.Fatalf("changed message reused: %#v, err=%v", got, err)
			}
		})
	}
}

func TestProbeReuseKeepsIdenticalMessageTrailers(t *testing.T) {
	const message = "Improve gate\n\nDirgate-Growth: cmd/ci-gates +1 #8847 pending split"
	routes := append([]mappedGHRoute{
		{match: "compare/" + testedBaseSHA + "..." + sourceMergeSHA, output: messageCompareJSON(sourceMergeSHA, reuseOldSHA, message)},
		{match: "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA, output: messageCompareJSON(targetMessageMergeSHA, reuseHeadSHA, message)},
	}, boundFullSourceRoutes(boundFullStatus())...)
	runner := &mappedGHRunner{routes: routes}
	ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
	got, err := probeReuse(ctx, runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || !got.Reuse {
		t.Fatalf("identical stored messages did not reuse: %#v, err=%v", got, err)
	}
	seen := 0
	for _, call := range runner.calls {
		for _, arg := range call {
			if strings.Contains(arg, "/compare/"+testedBaseSHA+"...") {
				seen++
				if !strings.Contains(arg, "?per_page=100&page=1") {
					t.Fatalf("compare did not page immutable range: %v", call)
				}
			}
		}
	}
	if seen != 2 {
		t.Fatalf("read %d immutable message ranges, want 2", seen)
	}
}

func TestProbeReuseRefusesIncompleteMessageEvidence(t *testing.T) {
	valid := messageCompareJSON(targetMessageMergeSHA, reuseHeadSHA, "safe")
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "missing message", raw: strings.Replace(valid, `"message":"safe"`, `"other":"safe"`, 1)},
		{name: "count mismatch", raw: strings.Replace(valid, `"total_commits":2`, `"total_commits":3`, 1)},
		{name: "missing synthetic", raw: strings.Replace(valid, targetMessageMergeSHA, reuseParentSHA, 1)},
		{name: "synthetic trailing newline", raw: strings.Replace(valid, " into "+testedBaseSHA, " into "+testedBaseSHA+`\n`, 1)},
		{name: "wrong synthetic parent", raw: strings.Replace(valid, " into "+testedBaseSHA, " into "+reuseParentSHA, 1)},
		{name: "missing head record", raw: strings.Replace(valid, reuseHeadSHA, reuseParentSHA, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := append([]mappedGHRoute{{match: "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA, output: tc.raw}}, boundFullSourceRoutes(boundFullStatus())...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
			got, err := probeReuse(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if err != nil || got.Reuse {
				t.Fatalf("incomplete message evidence reused: %#v, err=%v", got, err)
			}
		})
	}
}

func TestFullBackedSourceRefusesUnsafeHistoricalReuse(t *testing.T) {
	const rootMergeSHA = "8888888888888888888888888888888888888888"
	reuseDescription, _ := formatTransferProof(transferProof{Mode: "reuse", Input: testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA}, FullRootSHA: reuseParentSHA})
	rootDescription, _ := formatTransferProof(transferProof{Mode: "full", Input: testedInput{RunID: 103, Attempt: 1, MergeSHA: rootMergeSHA}})
	routes := boundFullSourceRoutes(`[ {"context":"required-gates-complete","state":"success","description":"` + reuseDescription + `"} ]`)
	for i := range routes {
		if routes[i].match == "actions/runs/101/attempts/1/jobs" {
			routes[i].output = reuseAttemptJobs()
		}
	}
	routes = append([]mappedGHRoute{
		{match: "commits/" + reuseParentSHA + "/statuses", output: `[ {"context":"required-gates-complete","state":"success","description":"` + rootDescription + `"} ]`},
		{match: "actions/runs/103/attempts/1/jobs", output: sourceFullJobs(true, false)},
		{match: "actions/runs/103", output: strings.Replace(testedRunJSON(reuseParentSHA, testedRefsJSON(rootMergeSHA), 1), `"id":101`, `"id":103`, 1)},
		{match: "git/commits/" + rootMergeSHA, output: testedCommitJSON(rootMergeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "compare/" + testedBaseSHA + "..." + rootMergeSHA, output: messageCompareJSON(rootMergeSHA, reuseParentSHA, "old safe message")},
	}, routes...)
	_, _, fullBacked, err := fullBackedSource(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseOldSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || fullBacked {
		t.Fatalf("historical unsafe reuse source remained full-backed: backed=%v, err=%v", fullBacked, err)
	}
}

func TestTestedMessagesRequiresEveryComparePage(t *testing.T) {
	makeRecord := func(sha, message string) map[string]any {
		return map[string]any{"sha": sha, "commit": map[string]any{"message": message}}
	}
	first := []map[string]any{makeRecord(reuseHeadSHA, "head")}
	for i := 1; i < 100; i++ {
		first = append(first, makeRecord(fmt.Sprintf("%040x", i), "same"))
	}
	encode := func(records []map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"total_commits": 101, "commits": records})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	input := testedInput{MergeSHA: targetMessageMergeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA}
	prefix := "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA + "?per_page=100&page="
	for _, tc := range []struct {
		name string
		last string
		want bool
	}{
		{name: "complete", last: encode([]map[string]any{makeRecord(targetMessageMergeSHA, "Merge "+reuseHeadSHA+" into "+testedBaseSHA)}), want: true},
		{name: "missing second page"},
		{name: "short second page", last: encode(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mappedGHRunner{routes: []mappedGHRoute{{match: prefix + "1", output: encode(first)}}}
			if tc.last != "" {
				runner.routes = append(runner.routes, mappedGHRoute{match: prefix + "2", output: tc.last})
			}
			messages, complete, err := testedMessages(context.Background(), runner, "eshu-hq/eshu", input)
			if complete != tc.want || (tc.want && (err != nil || messages["same"] != 99 || messages["head"] != 1)) {
				t.Fatalf("messages=%v complete=%v err=%v, want complete=%v", messages, complete, err, tc.want)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("read %d pages, want 2", len(runner.calls))
			}
		})
	}
}

func TestSameReuseMessagesPreservesDuplicateMultiplicity(t *testing.T) {
	encode := func(merge, head, older, second string) string {
		records := []map[string]any{
			{"sha": head, "commit": map[string]any{"message": "same"}},
			{"sha": older, "commit": map[string]any{"message": second}},
			{"sha": merge, "commit": map[string]any{"message": "Merge " + head + " into " + testedBaseSHA}},
		}
		raw, _ := json.Marshal(map[string]any{"total_commits": 3, "commits": records})
		return string(raw)
	}
	source := testedInput{MergeSHA: sourceMergeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseOldSHA}
	target := testedInput{MergeSHA: targetMessageMergeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA}
	for _, tc := range []struct {
		second string
		want   bool
	}{
		{second: "same", want: true},
		{second: "other"},
	} {
		runner := &mappedGHRunner{routes: []mappedGHRoute{
			{match: "compare/" + testedBaseSHA + "..." + sourceMergeSHA, output: encode(sourceMergeSHA, reuseOldSHA, reuseParentSHA, "same")},
			{match: "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA, output: encode(targetMessageMergeSHA, reuseHeadSHA, reuseTreeSHA, tc.second)},
		}}
		matched, err := sameReuseMessages(context.Background(), runner, "eshu-hq/eshu", source, target)
		if err != nil || matched != tc.want {
			t.Fatalf("second=%q matched=%v err=%v, want %v", tc.second, matched, err, tc.want)
		}
	}
}

func TestSameReuseMessagesRefusesChangedBaseWithoutAPIReads(t *testing.T) {
	runner := &mappedGHRunner{}
	source := testedInput{MergeSHA: sourceMergeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseOldSHA}
	target := testedInput{MergeSHA: targetMessageMergeSHA, BaseSHA: reuseParentSHA, HeadSHA: reuseHeadSHA}
	matched, err := sameReuseMessages(context.Background(), runner, "eshu-hq/eshu", source, target)
	if err != nil || matched || len(runner.calls) != 0 {
		t.Fatalf("changed base matched=%v err=%v calls=%v", matched, err, runner.calls)
	}
}
