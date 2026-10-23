// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mappedGHRunner answers the first route whose match substring appears in
// the joined gh arguments, so a test scripts endpoints, not call order.
type mappedGHRoute struct {
	match  string
	output string
	err    error
}

type mappedGHRunner struct {
	routes []mappedGHRoute
	calls  [][]string
}

func (r *mappedGHRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	for _, route := range r.routes {
		if strings.Contains(joined, route.match) {
			return []byte(route.output), route.err
		}
	}
	return nil, errors.New("unrouted gh invocation: " + joined)
}

const (
	reuseHeadSHA   = "1111111111111111111111111111111111111111"
	reuseOldSHA    = "2222222222222222222222222222222222222222"
	reuseParentSHA = "3333333333333333333333333333333333333333"
	reuseTreeSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reuseOtherTree = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func commitJSON(tree, parent string) string {
	if parent == "" {
		return `{"tree":{"sha":"` + tree + `"},"parents":[]}`
	}
	return `{"tree":{"sha":"` + tree + `"},"parents":[{"sha":"` + parent + `"}]}`
}

func forcePushJSON(before, after string) string {
	return `{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[` +
		`{"beforeCommit":{"oid":"` + before + `"},"afterCommit":{"oid":"` + after + `"}}]}}}}}`
}

func successRoutes(oldSHA string) []mappedGHRoute {
	return []mappedGHRoute{
		{match: "check-runs?check_name=go-core-complete", output: `[{"check_runs":[{"id":7,"conclusion":"success"}]}]`},
		{match: "check-runs?check_name=go-race-complete", output: `[{"check_runs":[{"id":3,"conclusion":"failure"}]},{"check_runs":[{"id":9,"conclusion":"success"}]}]`},
		{match: "check-runs?check_name=required-gates-complete", output: `[{"check_runs":[]}]`},
		{match: "commits/" + oldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + fullRunDescription + `"}]`},
	}
}

func TestProbeReuse_IdenticalTreeAfterForcePushReuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
	}, successRoutes(reuseOldSHA)...)}
	got, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeReuse returned error: %v", err)
	}
	if !got.Reuse || got.SourceSHA != reuseOldSHA || got.Tree != reuseTreeSHA {
		t.Fatalf("verdict = %#v; want reuse from %s at tree %s", got, reuseOldSHA, reuseTreeSHA)
	}
}

func TestProbeReuse_DifferentTreeDoesNotReuse(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
	}, successRoutes(reuseOldSHA)...)}
	got, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeReuse returned error: %v", err)
	}
	if got.Reuse {
		t.Fatalf("verdict = %#v; want no reuse on a different tree", got)
	}
}

func TestProbeReuse_FailedOldContextDoesNotReuse(t *testing.T) {
	t.Parallel()

	routes := successRoutes(reuseOldSHA)
	routes[1].output = `[{"check_runs":[{"id":9,"conclusion":"failure"}]}]`
	runner := &mappedGHRunner{routes: append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
	}, routes...)}
	got, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeReuse returned error: %v", err)
	}
	if got.Reuse {
		t.Fatalf("verdict = %#v; want no reuse when a required context failed", got)
	}
}

// A plain push on top of a proven head is the landing shape: no
// force-push event names this head, the first parent carries a different
// tree, and the probe must refuse so the landing head runs every gate in
// full. Refusal here is what lets a consumer change (a new await exit
// code, a new publisher arm) reach the default branch, where later heads
// can transfer verdicts against it.
func TestProbeReuse_PlainPushDifferentTreeDoesNotReuse(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: append([]mappedGHRoute{
		{match: "graphql", output: `{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[]}}}}}`},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
	}, successRoutes(reuseParentSHA)...)}
	got, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeReuse returned error: %v", err)
	}
	if got.Reuse {
		t.Fatalf("verdict = %#v; want no reuse on a plain push with a different tree", got)
	}
}

func TestProbeReuse_EmptyCommitParentReuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: append([]mappedGHRoute{
		{match: "graphql", output: `{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[]}}}}}`},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseTreeSHA, "")},
	}, successRoutes(reuseParentSHA)...)}
	got, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeReuse returned error: %v", err)
	}
	if !got.Reuse || got.SourceSHA != reuseParentSHA {
		t.Fatalf("verdict = %#v; want reuse from first parent %s", got, reuseParentSHA)
	}
}

// An empty context list must never read as vacuously green (F1): the
// probe errors before any gh call instead of returning reuse on a tree
// match with zero evidence.
func TestProbeReuse_EmptyContextsFailClosed(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{}
	_, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA, nil)
	if err == nil {
		t.Fatal("probeReuse with no contexts must error, never reuse")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("probeReuse made %d gh calls before rejecting empty contexts", len(runner.calls))
	}
}

// A non-SHA head is rejected before it can reach a REST path (F3).
func TestProbeReuse_RejectsNonSHAHead(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{}
	_, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, "../statuses", []string{"required-gates-complete"})
	if err == nil || !strings.Contains(err.Error(), "invalid --head-sha") {
		t.Fatalf("probeReuse error = %v; want the head-SHA shape rejection", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("probeReuse made %d gh calls before rejecting the head SHA", len(runner.calls))
	}
}

// The repo gate is the only thing between --repo and REST path
// interpolation: dot segments must not pass as an owner or name.
func TestValidRepoName(t *testing.T) {
	t.Parallel()

	for repo, want := range map[string]bool{
		"eshu-hq/eshu":    true,
		"owner.name/repo": true,
		"../x":            false,
		"x/..":            false,
		"./x":             false,
		"a../x":           false,
		"noslash":         false,
		"a/b/c":           false,
		"":                false,
	} {
		if got := validRepoName(repo); got != want {
			t.Errorf("validRepoName(%q) = %v, want %v", repo, got, want)
		}
	}
}

func TestProbeReuse_GHErrorFailsClosed(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: []mappedGHRoute{
		{match: "graphql", err: errors.New("gh exploded")},
	}}
	_, err := probeReuse(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"required-gates-complete"})
	if err == nil {
		t.Fatal("probeReuse must return the gh error so callers fall back to a normal run")
	}
}
