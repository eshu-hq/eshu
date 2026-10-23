// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

const (
	forkHeadSHA  = "e57dc86f774a8b85c82ef70d26fea3a14772ba74"
	forkMergeSHA = "16258fad44a053a42929d26ec4c545e5a4e68fbb"
	forkBaseSHA  = "88beb12d0324e0f676e89391ee09cc0a85c9f93c"
)

func TestReadCurrentCalledChecksBindsForkMergeCommit(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?":                             `[{"workflow_runs":[{"id":38768895788,"name":"Build Test","path":".github/workflows/test.yml","event":"pull_request","head_sha":"` + forkHeadSHA + `","run_attempt":2,"pull_requests":[ ],"referenced_workflows":[{"path":"eshu-hq/eshu/.github/workflows/queue-selection.yml@` + forkMergeSHA + `","sha":"` + forkMergeSHA + `","ref":"refs/pull/7818/merge"}]},{"id":38768895787,"name":"Build Test","path":".github/workflows/test.yml","event":"pull_request","head_sha":"` + forkHeadSHA + `","run_attempt":1,"pull_requests":[],"referenced_workflows":[{"path":"eshu-hq/eshu/.github/workflows/queue-selection.yml@` + forkMergeSHA + `","sha":"` + forkMergeSHA + `","ref":"refs/pull/7818/merge"}]}]}]`,
		"/git/commits/" + forkMergeSHA:              `{"sha":"` + forkMergeSHA + `","parents":[{"sha":"` + forkBaseSHA + `"},{"sha":"` + forkHeadSHA + `"}]}`,
		"/actions/runs/38768895788/attempts/2/jobs": `[{"jobs":[{"name":"call-frontend / console","status":"completed","conclusion":"failure","run_attempt":2}]}]`,
		"/actions/runs/38768895788/attempts/1/jobs": `[{"jobs":[{"name":"call-frontend / console","status":"completed","conclusion":"success","run_attempt":1}]}]`,
	}}
	checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", 7818, forkHeadSHA)
	if err != nil || len(checks) != 1 || checks[0].Bucket != "fail" || checks[0].RunID != 38768895788 {
		t.Fatalf("fork current attempt = %#v, err=%v", checks, err)
	}
	commitReads := 0
	for _, call := range runner.calls {
		if strings.Contains(call, "/attempts/1/jobs") || strings.Contains(call, "/git/ref/") {
			t.Fatalf("read mutable or obsolete fork evidence: %s", call)
		}
		if strings.Contains(call, "/git/commits/"+forkMergeSHA) {
			commitReads++
		}
	}
	if commitReads != 1 {
		t.Fatalf("immutable merge commit reads = %d, want one", commitReads)
	}
}

func TestReadCurrentCalledChecksForkBindingRefusesUntrustedEvidence(t *testing.T) {
	t.Parallel()
	queue := `{"path":"eshu-hq/eshu/.github/workflows/queue-selection.yml@` + forkMergeSHA + `","sha":"` + forkMergeSHA + `","ref":"refs/pull/7818/merge"}`
	other := `{"path":"eshu-hq/eshu/.github/workflows/other.yml@` + forkMergeSHA + `","sha":"` + forkMergeSHA + `","ref":"refs/pull/7818/merge"}`
	validCommit := `{"sha":"` + forkMergeSHA + `","parents":[{"sha":"` + forkBaseSHA + `"},{"sha":"` + forkHeadSHA + `"}]}`
	cases := []struct {
		name        string
		association string
		refs        string
		commit      string
		pr          int
		omitCommit  bool
		wantError   bool
	}{
		{name: "wrong or closed PR shared head", pr: 999},
		{name: "nonempty wrong PR association", association: `[{"number":999}]`},
		{name: "multiple PR associations", association: `[{"number":7818},{"number":999}]`},
		{name: "missing association", association: "<missing>"},
		{name: "null association", association: `null`},
		{name: "malformed association", association: `{}`},
		{name: "missing queue selection", refs: `[` + other + `]`},
		{name: "foreign direct workflow", refs: `[` + strings.Replace(queue, "eshu-hq/eshu/", "foreign/repo/", 1) + `]`},
		{name: "nested remote workflow", refs: `[` + queue + `,` + strings.Replace(other, "eshu-hq/eshu/.github/workflows/other.yml", "foreign/repo/.github/workflows/nested/other.yml", 1) + `]`},
		{name: "mixed merge refs", refs: `[` + queue + `,` + strings.Replace(other, "refs/pull/7818/merge", "refs/pull/999/merge", 1) + `]`},
		{name: "mixed merge SHAs", refs: `[` + queue + `,` + strings.ReplaceAll(other, forkMergeSHA, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") + `]`},
		{name: "duplicate reference", refs: `[` + queue + `,` + queue + `]`},
		{name: "remote at mutable ref", refs: `[` + queue + `,` + strings.Replace(other, "@"+forkMergeSHA, "@refs/heads/main", 1) + `]`},
		{name: "SHA only without PR ref", refs: `[` + strings.Replace(queue, "refs/pull/7818/merge", "", 1) + `]`},
		{name: "malformed referenced workflows", refs: `{}`},
		{name: "malformed immutable commit", commit: `{`},
		{name: "returned merge SHA differs", commit: `{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parents":[{"sha":"` + forkBaseSHA + `"},{"sha":"` + forkHeadSHA + `"}]}`},
		{name: "merge has one parent", commit: `{"sha":"` + forkMergeSHA + `","parents":[{"sha":"` + forkBaseSHA + `"}]}`},
		{name: "merge second parent differs", commit: `{"sha":"` + forkMergeSHA + `","parents":[{"sha":"` + forkBaseSHA + `"},{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`},
		{name: "immutable commit API error", omitCommit: true, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			association := tc.association
			if association == "" {
				association = `[]`
			}
			associationField := `"pull_requests":` + association + `,`
			if association == "<missing>" {
				associationField = ""
			}
			refs := tc.refs
			if refs == "" {
				refs = `[` + queue + `]`
			}
			commit := tc.commit
			if commit == "" {
				commit = validCommit
			}
			pr := tc.pr
			if pr == 0 {
				pr = 7818
			}
			run := `[{"workflow_runs":[{"id":38768895788,"name":"Build Test","path":".github/workflows/test.yml","event":"pull_request","head_sha":"` + forkHeadSHA + `","run_attempt":1,` + associationField + `"referenced_workflows":` + refs + `}]}]`
			routes := map[string]string{
				"actions/runs?": run,
				"/actions/runs/38768895788/attempts/1/jobs": `[{"jobs":[{"name":"call-frontend / console","status":"completed","conclusion":"success","run_attempt":1}]}]`,
			}
			if !tc.omitCommit {
				routes["/git/commits/"+forkMergeSHA] = commit
			}
			runner := &endpointRunner{routes: routes}
			checks, err := readCurrentCalledChecks(context.Background(), runner, "eshu-hq/eshu", pr, forkHeadSHA)
			if len(checks) != 0 {
				t.Fatalf("untrusted fork evidence passed: %#v", checks)
			}
			if tc.wantError && err == nil {
				t.Fatal("immutable commit API failure did not fail closed")
			}
			for _, call := range runner.calls {
				if strings.Contains(call, "/jobs") {
					t.Fatalf("queried jobs of unbound run: %s", call)
				}
			}
		})
	}
}
