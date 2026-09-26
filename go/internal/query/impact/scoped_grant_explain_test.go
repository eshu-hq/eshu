// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

const explainRoute = "/api/v0/impact/explain-dependency-path"

// E1: an ungranted or unowned endpoint returns the same 404 as an unknown one
// and issues no shortestPath.
func TestScopedExplainDependencyPathUngrantedEndpointIs404(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	unknown := postImpact(t, newTwoTenantHandler(&twoTenantGraph{resolveNothing: true}), explainRoute, `{"source":"cr-a","target":"x"}`, &a)
	for _, target := range []string{"cr-b", "bucket-b", "repo-b", "svc-b", "cr-orphan", "cidr-1", "platform-1"} {
		g := &twoTenantGraph{shortest: []string{"cr-a", "wi-a", "repo-a"}}
		rec := postImpact(t, newTwoTenantHandler(g), explainRoute, `{"source":"cr-a","target":"`+target+`"}`, &a)
		if rec.Code != http.StatusNotFound || rec.Body.String() != unknown.Body.String() {
			t.Errorf("target=%s: got %d %s, want the unknown-anchor %d %s", target, rec.Code, rec.Body.String(), unknown.Code, unknown.Body.String())
		}
		if n := len(g.callsOf("shortest")); n != 0 {
			t.Errorf("target=%s issued %d shortestPath statements, want 0", target, n)
		}
		assertNoTenantB(t, rec.Body.String())
	}
}

// E2: a path crossing a foreign interior renders like no path at all.
func TestScopedExplainDependencyPathForeignInteriorHasNoPath(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	for _, interior := range []string{"cr-b", "wi-b", "fn-b", "cr-orphan", "platform-1"} {
		g := &twoTenantGraph{shortest: []string{"cr-a", interior, "repo-a"}}
		rec := postImpact(t, newTwoTenantHandler(g), explainRoute, `{"source":"cr-a","target":"repo-a"}`, &a)
		data := testutil.DecodeImpactEnvelopeData(t, rec)
		noPath := postImpact(t, newTwoTenantHandler(&twoTenantGraph{}), explainRoute, `{"source":"cr-a","target":"repo-a"}`, &a)
		if rec.Body.String() != noPath.Body.String() {
			t.Errorf("interior=%s body\n got %s\nwant %s (indistinguishable from no path)", interior, rec.Body.String(), noPath.Body.String())
		}
		for _, key := range []string{"path", "confidence", "reason"} {
			if _, ok := data[key]; ok {
				t.Errorf("interior=%s response carries %q", interior, key)
			}
		}
		assertNoTenantB(t, rec.Body.String())
	}
}

// E3: a wholly granted request matches the shared-key response apart from the
// scoped disclosure fields.
func TestScopedExplainDependencyPathGrantedMatchesSharedKey(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	shared := auth.AuthContext{Mode: auth.AuthModeShared}
	path := []string{"cr-a", "wi-a", "repo-a"}
	scoped := testutil.DecodeImpactEnvelopeData(t, postImpact(t, newTwoTenantHandler(&twoTenantGraph{shortest: path}), explainRoute, `{"source":"cr-a","target":"repo-a"}`, &a))
	unscoped := testutil.DecodeImpactEnvelopeData(t, postImpact(t, newTwoTenantHandler(&twoTenantGraph{shortest: path}), explainRoute, `{"source":"cr-a","target":"repo-a"}`, &shared))
	if scoped["scoped"] != true {
		t.Fatalf("scoped = %#v, want true", scoped["scoped"])
	}
	delete(scoped, "scoped")
	delete(scoped, "withheld_sections")
	if !reflect.DeepEqual(scoped, unscoped) {
		a, _ := json.Marshal(scoped)
		b, _ := json.Marshal(unscoped)
		t.Fatalf("granted scoped response differs from shared-key\n scoped %s\nshared %s", a, b)
	}
	if _, ok := scoped["path"]; !ok {
		t.Fatalf("granted path missing: %#v", scoped)
	}
}

// E4: a DEPLOYMENT_SOURCE-rescued WorkloadInstance endpoint is admitted.
func TestScopedExplainDependencyPathRescuedEndpointAdmitted(t *testing.T) {
	t.Parallel()
	a := tenantAAuth()
	g := &twoTenantGraph{shortest: []string{"wi-rescued", "cr-a", "wi-a", "repo-a"}}
	rec := postImpact(t, newTwoTenantHandler(g), explainRoute, `{"source":"wi-rescued","target":"repo-a"}`, &a)
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	if _, ok := data["path"].(map[string]any); !ok {
		t.Fatalf("rescued endpoint path missing: %s", rec.Body.String())
	}
}

// T4 for explain: an empty grant makes zero graph calls.
func TestScopedExplainDependencyPathEmptyGrantMakesNoGraphCalls(t *testing.T) {
	t.Parallel()
	empty := testutil.ScopedTestAuthContext("tenant-none", nil)
	g := &twoTenantGraph{shortest: []string{"cr-a", "wi-a", "repo-a"}}
	rec := postImpact(t, newTwoTenantHandler(g), explainRoute, `{"source":"cr-a","target":"repo-a"}`, &empty)
	if n := len(g.callsOf("")); n != 0 {
		t.Fatalf("graph calls = %d, want 0", n)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an empty grant", rec.Code)
	}
}
