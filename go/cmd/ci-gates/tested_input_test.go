// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

const (
	testedMergeSHA = "4444444444444444444444444444444444444444"
	testedBaseSHA  = "5555555555555555555555555555555555555555"
	testedTreeSHA  = "6666666666666666666666666666666666666666"
)

func testedRunJSON(head, refs string, attempt int) string {
	return `{"id":101,"name":"Build Test","path":".github/workflows/test.yml","repository":{"full_name":"eshu-hq/eshu"},"event":"pull_request","status":"completed","conclusion":"success","head_sha":"` + head + `","run_attempt":` + strconv.Itoa(attempt) + `,"pull_requests":[{"number":42}],"referenced_workflows":` + refs + `}`
}

func testedRefsJSON(merge string) string {
	return `[{"path":"eshu-hq/eshu/.github/workflows/queue-selection.yml@` + merge + `","sha":"` + merge + `","ref":"refs/pull/42/merge"}]`
}

func testedCommitJSON(merge, head string) string {
	return `{"sha":"` + merge + `","tree":{"sha":"` + testedTreeSHA + `"},"parents":[{"sha":"` + testedBaseSHA + `"},{"sha":"` + head + `"}]}`
}

func TestReadTestedInputBindsExactRunAndImmutableMerge(t *testing.T) {
	t.Parallel()
	runner := &mappedGHRunner{routes: []mappedGHRoute{
		{match: "actions/runs/101", output: testedRunJSON(reuseHeadSHA, testedRefsJSON(testedMergeSHA), 2)},
		{match: "git/commits/" + testedMergeSHA, output: testedCommitJSON(testedMergeSHA, reuseHeadSHA)},
	}}
	got, err := readTestedInput(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA, 101, 2)
	if err != nil || got.MergeSHA != testedMergeSHA || got.MergeTree != testedTreeSHA || got.BaseSHA != testedBaseSHA || got.HeadSHA != reuseHeadSHA || got.RunID != 101 || got.Attempt != 2 {
		t.Fatalf("tested input = %#v, err=%v", got, err)
	}
	for _, args := range runner.calls {
		if strings.Contains(strings.Join(args, " "), "/git/ref/") {
			t.Fatalf("read mutable PR ref: %v", args)
		}
	}
}

func TestReadTestedInputRejectsUnboundEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		run     string
		commit  string
		attempt int
	}{
		{name: "stale attempt", run: testedRunJSON(reuseHeadSHA, testedRefsJSON(testedMergeSHA), 2), commit: testedCommitJSON(testedMergeSHA, reuseHeadSHA), attempt: 1},
		{name: "wrong head", run: testedRunJSON(reuseOldSHA, testedRefsJSON(testedMergeSHA), 1), commit: testedCommitJSON(testedMergeSHA, reuseHeadSHA), attempt: 1},
		{name: "wrong repo", run: strings.Replace(testedRunJSON(reuseHeadSHA, testedRefsJSON(testedMergeSHA), 1), `"full_name":"eshu-hq/eshu"`, `"full_name":"other/repo"`, 1), commit: testedCommitJSON(testedMergeSHA, reuseHeadSHA), attempt: 1},
		{name: "missing refs", run: testedRunJSON(reuseHeadSHA, `[]`, 1), commit: testedCommitJSON(testedMergeSHA, reuseHeadSHA), attempt: 1},
		{name: "wrong parent", run: testedRunJSON(reuseHeadSHA, testedRefsJSON(testedMergeSHA), 1), commit: testedCommitJSON(testedMergeSHA, reuseOldSHA), attempt: 1},
		{name: "wrong returned sha", run: testedRunJSON(reuseHeadSHA, testedRefsJSON(testedMergeSHA), 1), commit: testedCommitJSON(testedBaseSHA, reuseHeadSHA), attempt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mappedGHRunner{routes: []mappedGHRoute{
				{match: "actions/runs/101", output: tc.run},
				{match: "git/commits/" + testedMergeSHA, output: tc.commit},
			}}
			if got, err := readTestedInput(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA, 101, tc.attempt); err == nil {
				t.Fatalf("accepted unbound input: %#v", got)
			}
		})
	}
}
