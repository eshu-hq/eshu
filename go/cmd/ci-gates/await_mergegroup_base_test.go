// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestMergeGroupBaseSHAParsesTheQueueBranch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		branch  string
		baseRef string
		want    string
		wantErr string
	}{
		{"queue branch for the default branch", queueBranchFixture, "main", baseSHAFixture, ""},
		{"base branch with a slash", "gh-readonly-queue/release/1.x/pr-9-" + baseSHAFixture, "release/1.x", baseSHAFixture, ""},
		{"queue branch for another base", "gh-readonly-queue/dev/pr-9-" + baseSHAFixture, "main", "", "targets base"},
		{"not a queue branch", "feature/pr-7275-" + baseSHAFixture, "main", "", "not a merge queue branch"},
		{"empty branch", "", "main", "", "not a merge queue branch"},
		{"short sha", "gh-readonly-queue/main/pr-7275-23a97c2", "main", "", "not a merge queue branch"},
		{"uppercase sha", "gh-readonly-queue/main/pr-7275-" + strings.ToUpper(baseSHAFixture), "main", "", "not a merge queue branch"},
		{"missing pr number", "gh-readonly-queue/main/pr--" + baseSHAFixture, "main", "", "not a merge queue branch"},
	}
	for _, tc := range cases {
		got, err := mergeGroupBaseSHA(tc.branch, tc.baseRef)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v; want it to contain %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

// Regression for #7281: the queue fast-forwards main to the group head, and a
// late Required Gates run then compares main...head, which is empty. The base
// must be the group's fixed parent SHA, which still yields the real diff.
func TestMergeGroupChangedPathsSurviveMainAdvancingToTheGroupHead(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		// main already IS the group head, so the moving-ref compare is empty.
		"compare/main..." + headSHAFixture: `{"files":[]}`,
		"compare/" + baseSHAFixture + "..." + headSHAFixture: `{"files":[
			{"filename":"go/cmd/ci-gates/await_mergegroup.go"}
		]}`,
	}}

	got, truncated, err := mergeGroupChangedPaths(context.Background(), runner, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture)
	if err != nil {
		t.Fatalf("mergeGroupChangedPaths: %v", err)
	}
	if truncated {
		t.Fatal("one file is not a truncated compare")
	}
	if want := []string{"go/cmd/ci-gates/await_mergegroup.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v; want %v", got, want)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "compare/main") {
			t.Errorf("call %q compares a moving branch ref; it must compare the fixed base SHA", call)
		}
	}
}

func TestMergeGroupChangedPathsStillFailsOnAnEmptyDiffAgainstTheRealBase(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{"compare/": `{"files":[]}`}}
	_, _, err := mergeGroupChangedPaths(context.Background(), runner, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture)
	if err == nil || !strings.Contains(err.Error(), "no changed paths against base "+baseSHAFixture) {
		t.Fatalf("err = %v; an empty diff against the real base must fail closed, naming the base SHA", err)
	}
}

func TestMergeGroupChangedPathsFailsClosedWithoutADeterminableBase(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{"compare/": `{"files":[{"filename":"a.go"}]}`}}
	_, _, err := mergeGroupChangedPaths(context.Background(), runner, "eshu-hq/eshu", "main", "main", headSHAFixture)
	if err == nil || !strings.Contains(err.Error(), "not a merge queue branch") {
		t.Fatalf("err = %v; a branch that carries no base SHA must fail with a distinct error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %v; no compare may run when the base is unknown (it would fall back to a moving ref)", runner.calls)
	}
}
