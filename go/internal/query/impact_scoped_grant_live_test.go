// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// liveTwoTenantPrefix namespaces every node the live two-tenant fixture writes.
const liveTwoTenantPrefix = "i5167t:"

func lt(id string) string { return liveTwoTenantPrefix + id }

// liveTwoTenantEntityStore resolves the fixture Functions for trace-exposure-path,
// each classified as an HTTP handler.
type liveTwoTenantEntityStore struct {
	fakePortContentStore
}

var liveTwoTenantFunctions = map[string]string{"fn-a": "repo-a", "fn-a2": "repo-a", "fn-a3": "repo-a", "fn-b": "repo-b"}

func (liveTwoTenantEntityStore) GetEntityContent(_ context.Context, id string) (*EntityContent, error) {
	for fn, repo := range liveTwoTenantFunctions {
		if lt(fn) == id {
			return &EntityContent{
				EntityID: id, RepoID: lt(repo), EntityName: "Handler_" + fn, EntityType: "function",
				Metadata: map[string]any{"dead_code_root_kinds": []any{"go.net_http_handler_signature"}},
			}, nil
		}
	}
	return nil, nil
}

// seedLiveTwoTenantFixture writes the #5167 two-tenant fixture: repo-a is the
// granted tenant, repo-b the foreign one. DEPENDS_ON edges shape the
// resource-to-code and shortest paths; USES, MATCHES_STATE, and
// DEPLOYMENT_SOURCE are the ownership edges; CALLS and the sink edges shape the
// exposure walk.
func seedLiveTwoTenantFixture(t *testing.T, ctx context.Context, reader impactLiveReader) {
	t.Helper()
	exec := func(cypher string) {
		t.Helper()
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: strings.ReplaceAll(cypher, "$P", liveTwoTenantPrefix)}); err != nil {
			t.Fatalf("seed: %v\n%s", err, cypher)
		}
	}
	cleanup := func() {
		// The fixture is ~20 nodes, so one DETACH DELETE stays small.
		_, _ = reader.RunWrite(context.Background(), `MATCH (n) WHERE n.id STARTS WITH $p OR n.uid STARTS WITH $p DETACH DELETE n`,
			map[string]any{"p": liveTwoTenantPrefix})
	}
	cleanup()
	t.Cleanup(cleanup)
	exec(`CREATE (:Repository {id:'$Prepo-a', name:'$Psvc-a'}), (:Repository {id:'$Prepo-b', name:'$Psvc-b'}),
		(:WorkloadInstance {id:'$Pwi-a', name:'$Pinst-a', repo_id:'$Prepo-a'}),
		(:WorkloadInstance {id:'$Pwi-b', name:'$Pinst-b', repo_id:'$Prepo-b'}),
		(:WorkloadInstance {id:'$Pwi-rescued', name:'$Pinst-rescued', repo_id:'$Prepo-b'}),
		(:CloudResource {id:'$Pcr-a', uid:'$Pcr-a', name:'$Pbucket-a'}),
		(:CloudResource {id:'$Pcr-b', uid:'$Pcr-b', name:'$Pbucket-b'}),
		(:CloudResource {id:'$Pcr-shared', uid:'$Pcr-shared', name:'$Pbucket-shared'}),
		(:CloudResource {id:'$Pcr-orphan', uid:'$Pcr-orphan', name:'$Pbucket-orphan'}),
		(:TerraformResource {id:'$Ptf-b', uid:'$Ptf-b', name:'$Ptf-b', repo_id:'$Prepo-b'}),
		(:TerraformStateResource {id:'$Ptsr-b', uid:'$Ptsr-b', name:'$Pstate-b'}),
		(:CidrBlock {id:'$Pcidr-1', uid:'$Pcidr-1', name:'0.0.0.0/0', is_internet:true}),
		(:Function {id:'$Pfn-a', uid:'$Pfn-a', name:'$PHandleA', repo_id:'$Prepo-a'}),
		(:Function {id:'$Pfn-a2', uid:'$Pfn-a2', name:'$PhelperA2', repo_id:'$Prepo-a'}),
		(:Function {id:'$Pfn-a3', uid:'$Pfn-a3', name:'$PhelperA3', repo_id:'$Prepo-a'}),
		(:Function {id:'$Pfn-b', uid:'$Pfn-b', name:'$PhelperB', repo_id:'$Prepo-b'}),
		(:ShellCommand {id:'$Psh-a', uid:'$Psh-a', name:'ls', repo_id:'$Prepo-a'}),
		(:Workload {id:'$Pwl-a1', name:'$Pworkload-a1', repo_id:'$Prepo-a'}),
		(:Workload {id:'$Pwl-a2', name:'$Pworkload-a2', repo_id:'$Prepo-a'}),
		(:ShellCommand {id:'$Psh-shared', uid:'$Psh-shared', name:'curl', repo_id:'$Prepo-b'})`)
	edges := [][3]string{
		// ownership
		{"wi-a", "USES", "cr-a"},
		{"wi-b", "USES", "cr-b"},
		{"wi-a", "USES", "cr-shared"},
		{"wi-b", "USES", "cr-shared"},
		{"tf-b", "MATCHES_STATE", "tsr-b"},
		{"wi-rescued", "DEPLOYMENT_SOURCE", "repo-a"},
		// resource-to-code paths (depth 2)
		{"cr-a", "DEPENDS_ON", "wi-a"},
		{"wi-a", "DEPENDS_ON", "repo-a"},
		{"cr-a", "DEPENDS_ON", "cr-orphan"},
		{"cr-orphan", "DEPENDS_ON", "repo-a"},
		{"cr-a", "DEPENDS_ON", "wi-b"},
		{"wi-b", "DEPENDS_ON", "repo-a"},
		{"wi-b", "DEPENDS_ON", "repo-b"},
		{"cr-a", "DEPENDS_ON", "cr-b"},
		{"cr-b", "DEPENDS_ON", "repo-a"},
		{"cr-a", "DEPENDS_ON", "wi-rescued"},
		{"wi-rescued", "DEPENDS_ON", "repo-a"},
		{"cr-a", "DEPENDS_ON", "tsr-b"},
		{"tsr-b", "DEPENDS_ON", "repo-a"},
		{"cr-shared", "DEPENDS_ON", "wi-a"},
		{"cr-shared", "DEPENDS_ON", "wi-b"},
		// explain: the only wl-a1 .. wl-a2 path crosses repo-b's wi-b
		{"wl-a1", "DEPENDS_ON", "wi-b"},
		{"wi-b", "DEPENDS_ON", "wl-a2"},
		// exposure walk
		{"fn-a", "CALLS", "fn-a2"},
		{"fn-a", "CALLS", "fn-b"},
		{"fn-a3", "CALLS", "fn-b"},
		{"fn-b", "CALLS", "fn-a2"},
		{"fn-a2", "EXECUTES_SHELL", "sh-a"},
		{"fn-b", "EXECUTES_SHELL", "sh-a"},
		{"fn-a", "EXECUTES_SHELL", "sh-shared"},
		{"fn-a", "TO", "cidr-1"},
		{"fn-a", "CAN_PERFORM", "cr-a"},
		{"fn-a", "CAN_PERFORM", "cr-b"},
	}
	for _, edge := range edges {
		exec(fmt.Sprintf(`MATCH (a {id:'$P%s'}) MATCH (b {id:'$P%s'}) CREATE (a)-[:%s {confidence:0.9, reason:'fixture'}]->(b)`, edge[0], edge[2], edge[1]))
	}
}

// TestLiveImpactScopedGrantTwoTenant drives the three shipped handlers against
// a live two-tenant fixture (Neo4j or NornicDB) as a scoped repo-a caller
// (with a 1-id and a 130-id grant) and as a shared-key caller. Fakes cannot prove the grant
// predicates, so this is the backend proof for #5167 (T1-T3, T5, T6, E1-E4,
// X2-X5 on the real engine).
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 \
//		go test ./internal/query -run TestLiveImpactScopedGrantTwoTenant -count=1 -v
func TestLiveImpactScopedGrantTwoTenant(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live two-tenant impact grant proof")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	seedLiveTwoTenantFixture(t, ctx, reader)

	handler := &ImpactHandler{Neo4j: reader, Content: liveTwoTenantEntityStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	grant130 := []string{lt("repo-a")}
	for i := range 129 {
		grant130 = append(grant130, lt(fmt.Sprintf("filler-%03d", i)))
	}
	callers := map[string]*AuthContext{
		"scoped-1":   ptrAuth(testutil.ScopedTestAuthContext("tenant-a", []string{lt("repo-a")})),
		"scoped-130": ptrAuth(testutil.ScopedTestAuthContext("tenant-a", grant130)),
	}
	post := func(caller *AuthContext, path, body string) (int, string, map[string]any) {
		body = strings.ReplaceAll(body, "$P", liveTwoTenantPrefix)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Accept", EnvelopeMIMEType)
		if caller != nil {
			req = req.WithContext(ContextWithAuthContext(req.Context(), *caller))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var env ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		data, _ := env.Data.(map[string]any)
		return rec.Code, rec.Body.String(), data
	}
	foreign := []string{lt("cr-b"), lt("bucket-b"), lt("repo-b"), lt("svc-b"), lt("tsr-b"), lt("wi-b"), lt("fn-b"), lt("helperB"), lt("sh-shared")}
	noForeign := func(t *testing.T, body string, echo string) {
		t.Helper()
		if echo != "" {
			body = strings.Replace(body, `"start":{"id":"`+echo+`"}`, "", 1)
		}
		for _, id := range foreign {
			if strings.Contains(body, id) {
				t.Errorf("scoped body leaks %s: %s", id, body)
			}
		}
	}

	// Shared-key baseline: the unfiltered answer really contains the leaks.
	_, sharedBody, shared := post(nil, "/api/v0/impact/trace-resource-to-code", `{"start":"$Pcr-a","max_depth":2,"limit":200}`)
	if n := int(shared["count"].(float64)); n < 6 {
		t.Fatalf("shared-key baseline returned %d paths, want >= 6 (fixture not seeded as intended): %s", n, sharedBody)
	}
	if !strings.Contains(sharedBody, lt("repo-b")) {
		t.Fatalf("shared-key baseline does not reach repo-b; the negative control is vacuous: %s", sharedBody)
	}

	// Shared-key baselines for the other two routes, so each negative control
	// is proven non-vacuous on this engine.
	_, sharedBody, _ = post(nil, "/api/v0/impact/explain-dependency-path", `{"source":"$Pwl-a1","target":"$Pwl-a2"}`)
	if !strings.Contains(sharedBody, lt("wi-b")) {
		t.Fatalf("shared-key explain wl-a1 -> wl-a2 does not cross wi-b: %s", sharedBody)
	}
	// On Neo4j the exposure walk returns paths and the end-to-end sink
	// assertion below runs. The pinned NornicDB build returned no walk rows
	// (NornicDB-only); there the per-class ownership judgement below is the
	// proof.
	_, sharedBody, _ = post(nil, "/api/v0/impact/trace-exposure-path", `{"source_entity_id":"$Pfn-a","max_depth":3}`)
	exposureWalkWorks := strings.Contains(sharedBody, lt("sh-a"))
	t.Logf("shared-key exposure walk returns paths on this engine: %v", exposureWalkWorks)

	for name, caller := range callers {
		t.Run(name, func(t *testing.T) {
			// T5/T3: only the wi-a and wi-rescued paths survive.
			_, body, trace := post(caller, "/api/v0/impact/trace-resource-to-code", `{"start":"$Pcr-a","max_depth":2,"limit":200}`)
			if n := int(trace["count"].(float64)); n != 3 || trace["scoped"] != true {
				t.Errorf("cr-a scoped paths = %d scoped=%v, want 3 (via wi-a, and via wi-rescued over DEPENDS_ON and DEPLOYMENT_SOURCE): %s", n, trace["scoped"], body)
			}
			noForeign(t, body, "")
			// T2: shared resource returns only the repo-a path.
			_, body, trace = post(caller, "/api/v0/impact/trace-resource-to-code", `{"start":"$Pcr-shared","max_depth":2,"limit":200}`)
			if n := int(trace["count"].(float64)); n != 1 {
				t.Errorf("cr-shared scoped paths = %d, want 1: %s", n, body)
			}
			noForeign(t, body, "")
			// T3 truncation from the raw count.
			_, body, trace = post(caller, "/api/v0/impact/trace-resource-to-code", `{"start":"$Pcr-a","max_depth":2,"limit":1}`)
			if trace["truncated"] != true {
				t.Errorf("limit 1 truncated = %v, want true: %s", trace["truncated"], body)
			}
			// T1: ungranted anchor by id and by name == unknown anchor.
			for _, start := range []string{lt("cr-b"), lt("bucket-b"), lt("cr-orphan")} {
				code, got, _ := post(caller, "/api/v0/impact/trace-resource-to-code", `{"start":"`+start+`","max_depth":2}`)
				absent := start + "-absent"
				ucode, unknown, _ := post(caller, "/api/v0/impact/trace-resource-to-code", `{"start":"`+absent+`","max_depth":2}`)
				if code != ucode || got != strings.Replace(unknown, absent, start, 1) {
					t.Errorf("ungranted start %s rendered %d %s, want the unknown-anchor %d %s", start, code, got, ucode, unknown)
				}
				noForeign(t, got, start)
			}
			// E1: ungranted endpoints 404.
			for _, target := range []string{lt("cr-b"), lt("repo-b"), lt("cr-orphan"), lt("cidr-1")} {
				code, got, _ := post(caller, "/api/v0/impact/explain-dependency-path", `{"source":"$Pcr-a","target":"`+target+`"}`)
				if code != http.StatusNotFound {
					t.Errorf("explain target %s = %d %s, want 404", target, code, got)
				}
			}
			// E2: the only shortest path crosses repo-b's wi-b: no path.
			_, body, explain := post(caller, "/api/v0/impact/explain-dependency-path", `{"source":"$Pwl-a1","target":"$Pwl-a2"}`)
			if _, ok := explain["path"]; ok || explain["source"] == nil {
				t.Errorf("wl-a1 -> wl-a2 through wi-b returned a path (or no answer): %s", body)
			}
			noForeign(t, body, "")
			// E3: a wholly granted path matches the shared-key answer.
			_, _, scopedExplain := post(caller, "/api/v0/impact/explain-dependency-path", `{"source":"$Pwi-a","target":"$Prepo-a"}`)
			_, _, sharedExplain := post(nil, "/api/v0/impact/explain-dependency-path", `{"source":"$Pwi-a","target":"$Prepo-a"}`)
			delete(scopedExplain, "scoped")
			delete(scopedExplain, "withheld_sections")
			a, _ := json.Marshal(scopedExplain)
			b, _ := json.Marshal(sharedExplain)
			if string(a) != string(b) || !strings.Contains(string(a), `"path"`) {
				t.Errorf("granted explain differs from shared-key\n scoped %s\nshared %s", a, b)
			}
			// E4: a DEPLOYMENT_SOURCE-rescued endpoint is admitted.
			_, body, explain = post(caller, "/api/v0/impact/explain-dependency-path", `{"source":"$Pwi-rescued","target":"$Prepo-a"}`)
			if _, ok := explain["path"]; !ok {
				t.Errorf("rescued endpoint returned no path: %s", body)
			}
			// X1/X2-X5.
			_, body, exposureData := post(caller, "/api/v0/impact/trace-exposure-path", `{"source_entity_id":"$Pfn-b"}`)
			if paths, _ := exposureData["paths"].([]any); len(paths) != 0 {
				t.Errorf("foreign exposure source walked: %s", body)
			}
			_, body, exposureData = post(caller, "/api/v0/impact/trace-exposure-path", `{"source_entity_id":"$Pfn-a","max_depth":3}`)
			var sinks []string
			paths, _ := exposureData["paths"].([]any)
			for _, raw := range paths {
				sink := raw.(map[string]any)["sink"].(map[string]any)["node"].(map[string]any)
				sinks = append(sinks, sink["entity_id"].(string))
			}
			slices.Sort(sinks)
			if exposureWalkWorks {
				if want := []string{lt("cr-a"), lt("sh-a")}; !slices.Equal(sinks, want) {
					t.Errorf("exposure sinks = %v, want %v: %s", sinks, want, body)
				}
			}
			// Live per-class sink and chain judgement (X2-X5 on the engine).
			access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: caller.AllowedRepositoryIDs}
			checker := ownership.Checker{Graph: reader, Route: ownership.RouteTraceExposurePath}
			node := func(label, id, repo string) ownership.Node {
				return ownership.Node{ID: lt(id), UID: lt(id), RepoID: repo, Labels: []string{label}}
			}
			admit := []ownership.Node{node("CloudResource", "cr-a", ""), node("CloudResource", "cr-shared", ""), node("ShellCommand", "sh-a", lt("repo-a")), node("Function", "fn-a2", lt("repo-a")), node("WorkloadInstance", "wi-rescued", lt("repo-b"))}
			deny := []ownership.Node{node("CloudResource", "cr-b", ""), node("CloudResource", "cr-orphan", ""), node("TerraformStateResource", "tsr-b", ""), node("ShellCommand", "sh-shared", lt("repo-b")), node("Function", "fn-b", lt("repo-b")), node("CidrBlock", "cidr-1", ""), node("SecretsIAMSecretMetadataPath", "secret-1", ""), node("WorkloadInstance", "wi-b", lt("repo-b"))}
			verdict, err := checker.Check(ctx, access, append(append([]ownership.Node{}, admit...), deny...))
			if err != nil {
				t.Fatalf("live ownership check: %v", err)
			}
			for _, n := range admit {
				if !verdict.Admits(n) {
					t.Errorf("live ownership denied owned %s %s", n.Labels[0], n.ID)
				}
			}
			for _, n := range deny {
				if verdict.Admits(n) {
					t.Errorf("live ownership admitted foreign %s %s", n.Labels[0], n.ID)
				}
			}
			if !strings.Contains(body, "CidrBlock") || !strings.Contains(body, "SecretsIAMSecretMetadataPath") {
				t.Errorf("exposure response does not name the withheld sink classes: %s", body)
			}
			noForeign(t, body, "")
		})
	}
}
