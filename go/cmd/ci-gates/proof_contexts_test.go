// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// transferProofRegistry mirrors the live manifest: the two Go umbrellas are
// ruleset-required, the aggregate publisher is not (#9089).
func transferProofRegistry() *cigates.Registry {
	return &cigates.Registry{RequiredStatusChecks: []cigates.RequiredStatusCheck{
		{Context: "go-core-complete", RulesetRequired: true},
		{Context: "go-race-complete", RulesetRequired: true},
		{Context: "required-gates-complete", RulesetRequired: false},
	}}
}

// The transfer paths (reuse, smoke, pr-plan) judge a PREVIOUS head, and the
// aggregate's status description carries the proof they transfer. #9094 asked
// whether they should narrow to the ruleset set: they must not, because the
// aggregate is the proof carrier, not a merge gate. This pins that decision.
func TestTransferProofContextsKeepsAggregateOutsideRuleset(t *testing.T) {
	t.Parallel()

	got := transferProofContexts(transferProofRegistry())
	want := []string{"go-core-complete", "go-race-complete", "required-gates-complete"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transferProofContexts = %v, want %v", got, want)
	}
}

// transferProbeRoutes scripts a candidate head whose two ruleset-required
// umbrellas are green while the aggregate (not ruleset-required) is red. A
// probe that reads the whole manifest list sees the aggregate fail and stops
// at requiredContextsGreen; one narrowed to the ruleset subset passes that step.
func transferProbeRoutes() []mappedGHRoute {
	return append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"failure","description":"red"}]`},
	}, successRoutes(reuseOldSHA)...)
}

func transferProbeContext() context.Context {
	return withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
}

// readsAggregate reports whether the probe asked for the aggregate's check runs,
// which only happens when it holds the aggregate in its context list.
func readsAggregate(runner *mappedGHRunner) bool {
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "check-runs?check_name=required-gates-complete") {
			return true
		}
	}
	return false
}

// await, reuse and pr-plan reach the transfer probes through
// probeReuseForAwait and probeSmokeForAwait. Each must judge the candidate
// against the FULL manifest list (#9094): a candidate whose aggregate is red
// donates nothing, and the probe must read the aggregate to know that. Narrowing
// a call site to the ruleset subset makes the reads below disappear.
func TestTransferProbesJudgeCandidateAgainstFullManifestList(t *testing.T) {
	t.Parallel()

	reg := transferProofRegistry()
	t.Run("reuse", func(t *testing.T) {
		t.Parallel()
		runner := &mappedGHRunner{routes: transferProbeRoutes()}
		got, err := probeReuseForAwait(transferProbeContext(), runner, reg, "eshu-hq/eshu", 42, reuseHeadSHA)
		if err != nil || got.Reuse {
			t.Fatalf("reuse = %#v, err = %v; want no reuse from a candidate with a red aggregate", got, err)
		}
		if !readsAggregate(runner) {
			t.Fatalf("reuse probe never read the aggregate; calls = %v", runner.calls)
		}
	})
	t.Run("smoke", func(t *testing.T) {
		t.Parallel()
		runner := &mappedGHRunner{routes: transferProbeRoutes()}
		got, err := probeSmokeForAwait(transferProbeContext(), runner, reg, "eshu-hq/eshu", 42, reuseHeadSHA, []string{"docs/x.md"}, false)
		if err != nil || got.Applied {
			t.Fatalf("smoke = %#v, err = %v; want no smoke from a candidate with a red aggregate", got, err)
		}
		if !readsAggregate(runner) {
			t.Fatalf("smoke probe never read the aggregate; calls = %v", runner.calls)
		}
	})
}
