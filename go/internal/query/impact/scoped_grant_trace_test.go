// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

const traceRoute = "/api/v0/impact/trace-resource-to-code"

// traceFixturePaths is every resource-to-code path the fake backend knows.
func traceFixturePaths() [][]string {
	return [][]string{
		{"cr-a", "wi-a", "repo-a"},                       // T5 kept: crA -> ia -> repo-a
		{"cr-a", "cr-orphan", "repo-a"},                  // T5 dropped: orphan CloudResource interior
		{"cr-a", "wi-b", "repo-a"},                       // T3 dropped: foreign WorkloadInstance interior
		{"cr-a", "cr-b", "repo-a"},                       // T3 dropped: foreign CloudResource interior
		{"cr-a", "wi-rescued", "repo-a"},                 // kept: DEPLOYMENT_SOURCE-rescued interior
		{"cr-a", "tsr-b", "repo-a"},                      // dropped: TSR matched only by a repo-b TerraformResource
		{"cr-a", "platform-1", "repo-a"},                 // dropped: no-owner class
		{"cr-a", "wi-b", "repo-b"},                       // terminal repo-b: bound in Cypher
		{"cr-shared", "wi-a", "repo-a"},                  // T2 kept
		{"cr-shared", "wi-b", "repo-b"},                  // T2 terminal repo-b
		{"cr-b", "wi-b", "repo-b"},                       // T1 anchor repo-b's
		{"cr-orphan", "wi-a", "repo-a"},                  // anchor with no owner
		{"cr-a", "wi-a", "platform-1", "wi-a", "repo-a"}, // dropped: no-owner interior at depth
	}
}

func traceRepoIDs(t *testing.T, data map[string]any) []string {
	t.Helper()
	paths, _ := data["paths"].([]any)
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path, _ := raw.(map[string]any)
		id, _ := path["repo_id"].(string)
		out = append(out, id)
	}
	return out
}

// T1: an ungranted anchor, by id and by name, renders byte-identical to an
// unknown anchor and issues no traversal.
func TestScopedTraceResourceToCodeUngrantedAnchorIsUnknown(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	for _, start := range []string{"cr-b", "bucket-b", "cr-orphan", "platform-1"} {
		g := &twoTenantGraph{tracePaths: traceFixturePaths()}
		got := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"`+start+`"}`, &a)
		unknown := postImpact(t, newTwoTenantHandler(&twoTenantGraph{resolveNothing: true}), traceRoute, `{"start":"`+start+`"}`, &a)
		gotBody, wantBody := got.Body.String(), unknown.Body.String()
		if got.Code != unknown.Code || gotBody != wantBody {
			t.Errorf("start=%s scoped response\n got %d %s\nwant %d %s (identical to an unknown anchor)", start, got.Code, gotBody, unknown.Code, wantBody)
		}
		if n := len(g.callsOf("trace")); n != 0 {
			t.Errorf("start=%s issued %d traversals for an ungranted anchor, want 0", start, n)
		}
		// The only place the caller's own start string may appear is its
		// verbatim echo, which an unknown anchor carries too.
		assertNoTenantB(t, strings.Replace(gotBody, `"start":{"id":"`+start+`"}`, "", 1))
	}
}

// T2: crShared returns only the repo-a path.
func TestScopedTraceResourceToCodeSharedResourceReturnsOnlyGrantedRepo(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := &twoTenantGraph{tracePaths: traceFixturePaths()}
	rec := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"cr-shared"}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if got := traceRepoIDs(t, data); !slices.Equal(got, []string{"repo-a"}) {
		t.Fatalf("repo ids = %v, want [repo-a]", got)
	}
	if data["scoped"] != true {
		t.Fatalf("scoped = %#v, want true", data["scoped"])
	}
	assertNoTenantB(t, rec.Body.String())
}

// T3 + T5: paths through a foreign or unowned interior are dropped whole;
// the owned and rescued ones are kept; truncated comes from the raw count; the
// A1 statement's uid param is exactly the page's deduplicated CloudResource uids.
func TestScopedTraceResourceToCodeDropsUngrantedInteriorPaths(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := &twoTenantGraph{tracePaths: traceFixturePaths()}
	rec := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"cr-a","limit":200}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if got := traceRepoIDs(t, data); !slices.Equal(got, []string{"repo-a", "repo-a"}) {
		t.Fatalf("repo ids = %v, want the wi-a and wi-rescued paths only", got)
	}
	if data["truncated"] != false {
		t.Fatalf("truncated = %#v, want false (raw page under the limit)", data["truncated"])
	}
	assertNoTenantB(t, rec.Body.String())

	var crUIDs []string
	for _, call := range g.callsOf("ownership") {
		if uids, ok := call.params["uids"].([]string); ok && slices.Contains(uids, "cr-a") {
			crUIDs = uids
		}
	}
	slices.Sort(crUIDs)
	if want := []string{"cr-a", "cr-b", "cr-orphan"}; !slices.Equal(crUIDs, want) {
		t.Fatalf("A1 uids = %v, want exactly the deduplicated CloudResource uids %v", crUIDs, want)
	}
}

// T3 truncation: a raw page over the limit reports truncated even when the
// Go filter leaves fewer rows than the limit.
func TestScopedTraceResourceToCodeTruncatedFromRawCount(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := &twoTenantGraph{tracePaths: traceFixturePaths()}
	rec := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"cr-a","limit":2}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if data["truncated"] != true {
		t.Fatalf("truncated = %#v, want true (raw rows exceeded the limit)", data["truncated"])
	}
	if got := traceRepoIDs(t, data); !slices.Equal(got, []string{"repo-a"}) {
		t.Fatalf("repo ids = %v, want only the first page's granted path", got)
	}
}

// T4: an empty grant makes zero graph calls.
func TestScopedTraceResourceToCodeEmptyGrantMakesNoGraphCalls(t *testing.T) {
	t.Parallel()
	empty := testutil.ScopedTestAuthContext("tenant-none", nil)
	g := &twoTenantGraph{tracePaths: traceFixturePaths()}
	rec := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"cr-a"}`, &empty)
	if n := len(g.callsOf("")); n != 0 {
		t.Fatalf("graph calls = %d, want 0 for an empty grant", n)
	}
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if got := traceRepoIDs(t, data); len(got) != 0 {
		t.Fatalf("repo ids = %v, want none", got)
	}
}

// An unscoped (shared-key) caller keeps the pre-#5167 statement and rows.
func TestUnscopedTraceResourceToCodeIsUnchanged(t *testing.T) {
	t.Parallel()
	g := &twoTenantGraph{tracePaths: traceFixturePaths()}
	shared := auth.AuthContext{Mode: auth.AuthModeShared}
	rec := postImpact(t, newTwoTenantHandler(g), traceRoute, `{"start":"cr-shared"}`, &shared)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if got := traceRepoIDs(t, data); !slices.Equal(got, []string{"repo-a", "repo-b"}) {
		t.Fatalf("repo ids = %v, want both repos for an unscoped caller", got)
	}
	if _, ok := data["scoped"]; ok {
		t.Fatalf("unscoped response carries scoped: %#v", data["scoped"])
	}
	if n := len(g.callsOf("ownership")); n != 0 {
		t.Fatalf("ownership statements = %d, want 0 for an unscoped caller", n)
	}
}
