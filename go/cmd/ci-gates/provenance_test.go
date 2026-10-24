// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

func TestProbeReuseRefusesUnprovenSource(t *testing.T) {
	for _, description := range []string{smokeDescription, reuseDescription, "unknown"} {
		t.Run(description, func(t *testing.T) {
			routes := append([]mappedGHRoute{
				{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
				{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
				{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
				{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
				{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + description + `"}]`},
			}, successRoutes(reuseOldSHA)...)
			got, err := probeReuse(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA, []string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Reuse {
				t.Fatalf("source %q was reused", description)
			}
		})
	}
}

func TestProbeReuseRefusesUnboundV2Source(t *testing.T) {
	routes := append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + reuseV2Description + `"}]`},
	}, successRoutes(reuseOldSHA)...)
	got, err := probeReuse(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA, []string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || got.Reuse {
		t.Fatalf("unbound v2 source transferred: verdict=%+v, error=%v", got, err)
	}
}

func TestEndpointTreeDeltaDetectsBothSidesAndMetadata(t *testing.T) {
	routes := []mappedGHRoute{
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "git/trees/" + reuseTreeSHA, output: `{"truncated":false,"tree":[{"path":"old-name","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"mode-changed","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
		{match: "git/trees/" + reuseOtherTree, output: `{"truncated":false,"tree":[{"path":"new-name","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"mode-changed","mode":"100755","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
	}
	paths, complete, err := endpointTreeDelta(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", reuseOldSHA, reuseHeadSHA)
	if err != nil || !complete {
		t.Fatalf("endpoint delta complete=%v error=%v", complete, err)
	}
	want := map[string]bool{"old-name": true, "new-name": true, "mode-changed": true}
	for _, path := range paths {
		if !want[path] {
			t.Fatalf("unexpected changed path %q", path)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("missing changed paths %v", want)
	}
}

func TestReadEndpointTreeIncompleteRefuses(t *testing.T) {
	for _, response := range []string{
		`{"truncated":true,"tree":[]}`,
		`{"tree":[]}`,
		`{"truncated":false}`,
		`{"truncated":false,"tree":[{"path":"a","mode":"100644","type":"blob"}]}`,
		`{"truncated":false,"tree":[{"path":"a","mode":"040000","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`,
		`{"truncated":false,"tree":[{"path":"a","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"a","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`,
	} {
		runner := &mappedGHRunner{routes: []mappedGHRoute{{match: "git/trees/", output: response}}}
		_, complete, err := readEndpointTree(context.Background(), runner, "eshu-hq/eshu", reuseTreeSHA)
		if err != nil || complete {
			t.Fatalf("response %s: complete=%v error=%v", response, complete, err)
		}
	}
}

func TestProbeSmokeUnchangedEndpointWorkflowIgnoresThreeDotNoise(t *testing.T) {
	routes := append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "compare/", output: compareJSON(".github/workflows/required-gates.yml")},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "git/trees/" + reuseTreeSHA, output: `{"truncated":false,"tree":[{"path":".github/workflows/required-gates.yml","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
		{match: "git/trees/" + reuseOtherTree, output: `{"truncated":false,"tree":[{"path":".github/workflows/required-gates.yml","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"docs/new.md","mode":"100644","type":"blob","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`},
		{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + fullRunDescription + `"}]`},
	}, successRoutes(reuseOldSHA)...)
	got, err := probeSmoke(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA, []string{"go/cmd/ci-gates/reuse.go"}, false, []string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("unbound historical source transferred despite missing v3 proof")
	}
}

func TestEndpointTreeDeltaDetectsTypeAndSHA(t *testing.T) {
	routes := []mappedGHRoute{
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "git/trees/" + reuseTreeSHA, output: `{"truncated":false,"tree":[{"path":"type-changed","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"sha-changed","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
		{match: "git/trees/" + reuseOtherTree, output: `{"truncated":false,"tree":[{"path":"type-changed","mode":"160000","type":"commit","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"sha-changed","mode":"100644","type":"blob","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`},
	}
	paths, complete, err := endpointTreeDelta(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", reuseOldSHA, reuseHeadSHA)
	if err != nil || !complete || len(paths) != 2 {
		t.Fatalf("type/SHA delta paths=%v complete=%v error=%v", paths, complete, err)
	}
}

func TestProbeSmokeDeletedProtectedOrPRPathRefuses(t *testing.T) {
	for _, path := range []string{".github/workflows/check.yml", "go/cmd/ci-gates/reuse.go"} {
		t.Run(path, func(t *testing.T) {
			oldTree := `{"truncated":false,"tree":[{"path":"` + path + `","mode":"100644","type":"blob","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`
			routes := append([]mappedGHRoute{
				{match: "git/trees/" + reuseTreeSHA, output: oldTree},
				{match: "git/trees/" + reuseOtherTree, output: `{"truncated":false,"tree":[]}`},
			}, smokeRoutes(compareJSON())...)
			got, err := probeSmoke(context.Background(), &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go/cmd/ci-gates/reuse.go"}, false,
				[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if err != nil || got {
				t.Fatalf("deleted path %q: smoke=%v error=%v", path, got, err)
			}
		})
	}
}
