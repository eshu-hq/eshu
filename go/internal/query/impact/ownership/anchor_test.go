// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// D1: a scoped caller's anchor is the first candidate the grant owns, judged
// by one Check; an ungranted-only candidate set is nil; an unscoped caller
// uses the single-row resolve; an empty grant calls neither.
func TestResolveAnchorPicksFirstGrantedCandidate(t *testing.T) {
	t.Parallel()
	foreign := deployment.ResolvedImpactAnchor{ID: "cr-1", UID: "cr-1", Label: "CloudResource", Labels: []string{"CloudResource"}}
	owned := deployment.ResolvedImpactAnchor{ID: "cr-2", UID: "cr-2", Label: "CloudResource", Labels: []string{"CloudResource"}}
	single := &deployment.ResolvedImpactAnchor{ID: "cr-1"}
	resolveCalls, candidateCalls := 0, 0
	resolve := func() (*deployment.ResolvedImpactAnchor, error) { resolveCalls++; return single, nil }
	candidatesOf := func(list ...deployment.ResolvedImpactAnchor) func() ([]deployment.ResolvedImpactAnchor, error) {
		return func() ([]deployment.ResolvedImpactAnchor, error) { candidateCalls++; return list, nil }
	}

	g := &recordingGraph{admit: map[string]bool{"cr-2": true}, foreign: map[string]bool{"cr-1": true}}
	got, err := Checker{Graph: g}.ResolveAnchor(context.Background(), grantA(), resolve, candidatesOf(foreign, owned))
	if err != nil || got == nil || got.ID != "cr-2" {
		t.Fatalf("scoped shared name resolved %+v, %v; want cr-2", got, err)
	}
	if len(g.calls) != 1 || resolveCalls != 0 {
		t.Fatalf("statements = %d, single resolves = %d; want one Check over all candidates and no single resolve", len(g.calls), resolveCalls)
	}

	got, err = Checker{Graph: g}.ResolveAnchor(context.Background(), grantA(), resolve, candidatesOf(foreign))
	if err != nil || got != nil {
		t.Fatalf("ungranted-only candidates resolved %+v, %v; want nil", got, err)
	}

	candidateCalls = 0
	got, err = Checker{Graph: g}.ResolveAnchor(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, resolve, candidatesOf(owned))
	if err != nil || got != single || candidateCalls != 0 {
		t.Fatalf("unscoped resolved %+v (candidate calls %d), %v; want the single-row resolve", got, candidateCalls, err)
	}

	empty := querycontract.RepositoryAccessFilter{}
	resolveCalls, candidateCalls = 0, 0
	got, err = Checker{Graph: g}.ResolveAnchor(context.Background(), empty, resolve, candidatesOf(owned))
	if err != nil || got != nil || resolveCalls+candidateCalls != 0 {
		t.Fatalf("empty grant resolved %+v with %d calls, %v; want nil and no call", got, resolveCalls+candidateCalls, err)
	}
}
