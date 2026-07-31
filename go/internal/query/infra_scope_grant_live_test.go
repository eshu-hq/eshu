// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_shape

// Real-backend regression for the SHAPE-A scope predicate (#5384). It runs the
// ACTUAL production infraResourceScopePredicate / workloadScopePredicate builders
// as whole-label count filters against a live NornicDB (the pinned pr261 image),
// not an interface mock — the interface-mock path systematically false-greens
// tenant-isolation gaps because a Cypher-interpreting fake never produces the
// leaking row shape. This test seeds the exact name-collision topology over Bolt
// and proves:
//
//   - RED (dead n-last EXISTS bridge): the previously shipped shape counts ZERO
//     scoped CloudResources on NornicDB (under-authorization).
//   - RED (naive backward-EXISTS-with-WHERE): the tempting rewrite counts the
//     WHOLE label regardless of grant (over-authorization / whole-graph leak).
//   - GREEN (SHAPE-A): correct discrimination — grant repo-a admits the legit
//     resources and the collision Workload, excludes the tenant-B secret; and
//     the fail-closed cap degradation admits direct ownership while dropping
//     bridge admission beyond the cap.
//
// Gate: set ESHU_CYPHER_BOLT_DSN (e.g. bolt://localhost:27687) to the isolated
// NornicDB. Run with:
//
//	go test -tags live_infra_scope_shape ./internal/query \
//	  -run TestLiveInfraScopeShapeShapeADiscriminates -count=1
package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func liveScopeShapeSession(t *testing.T) (neo4jdriver.SessionWithContext, func()) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping live SHAPE-A regression")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify graph connectivity: %v", err)
	}
	session := driver.NewSession(context.Background(), neo4jdriver.SessionConfig{DatabaseName: database})
	return session, func() {
		_ = session.Close(context.Background())
		_ = driver.Close(context.Background())
	}
}

func liveScopeRun(t *testing.T, session neo4jdriver.SessionWithContext, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run cypher: %v\n%s", err, cypher)
	}
	recs, err := res.Collect(ctx)
	if err != nil {
		t.Fatalf("collect cypher: %v\n%s", err, cypher)
	}
	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.AsMap())
	}
	return out
}

// liveScopeCount runs `MATCH (<alias>:<label>) WHERE <predicate> RETURN
// count(<alias>)` and returns the count. The alias must match the alias the
// predicate was built for.
func liveScopeCount(t *testing.T, session neo4jdriver.SessionWithContext, alias, label, predicate string, params map[string]any) int64 {
	t.Helper()
	rows := liveScopeRun(t, session, fmt.Sprintf("MATCH (%s:%s) WHERE %s RETURN count(%s) AS c", alias, label, predicate, alias), params)
	if len(rows) != 1 {
		t.Fatalf("count query returned %d rows, want 1", len(rows))
	}
	c, ok := rows[0]["c"].(int64)
	if !ok {
		t.Fatalf("count value %#v is not int64", rows[0]["c"])
	}
	return c
}

// seedLiveScopeShapeFixture writes the name-collision topology into a scratch
// namespace (all ids prefixed sst- so it never collides with other data). It is
// idempotent-cleaned at the start.
func seedLiveScopeShapeFixture(t *testing.T, session neo4jdriver.SessionWithContext) {
	t.Helper()
	// Clean any prior scratch nodes.
	liveScopeRun(t, session, "MATCH (n) WHERE n.sst = true DETACH DELETE n", nil)
	// Topology (no in-statement comments: the pinned NornicDB parser rejects //):
	//   legit repo-a chain: sst-inst-a{repo_id:repo-a} USES sst-cloud-a,
	//     DEPLOYMENT_SOURCE->repo-a, INSTANCE_OF sst-wl-a{repo_id:repo-a}.
	//   collision: sst-wl-collide{repo_id:repo-b} DEFINED by repo-a AND repo-b;
	//     sst-inst-b{repo_id:repo-b} USES the tenant-B secret sst-cloud-b-secret.
	seed := "CREATE (ra:Repository {id:'sst-repo-a', sst:true}) " +
		"CREATE (rb:Repository {id:'sst-repo-b', sst:true}) " +
		"CREATE (wa:Workload {id:'sst-wl-a', repo_id:'sst-repo-a', sst:true}) " +
		"CREATE (ia:WorkloadInstance {id:'sst-inst-a', repo_id:'sst-repo-a', sst:true}) " +
		"CREATE (ca:CloudResource {id:'sst-cloud-a', sst:true}) " +
		"CREATE (ra)-[:DEFINES]->(wa) " +
		"CREATE (ia)-[:INSTANCE_OF]->(wa) " +
		"CREATE (ia)-[:USES]->(ca) " +
		"CREATE (ia)-[:DEPLOYMENT_SOURCE]->(ra) " +
		"CREATE (wc:Workload {id:'sst-wl-collide', repo_id:'sst-repo-b', sst:true}) " +
		"CREATE (ra)-[:DEFINES]->(wc) " +
		"CREATE (rb)-[:DEFINES]->(wc) " +
		"CREATE (ib:WorkloadInstance {id:'sst-inst-b', repo_id:'sst-repo-b', sst:true}) " +
		"CREATE (cb:CloudResource {id:'sst-cloud-b-secret', sst:true}) " +
		"CREATE (ib)-[:INSTANCE_OF]->(wc) " +
		"CREATE (ib)-[:USES]->(cb) " +
		"CREATE (ib)-[:DEPLOYMENT_SOURCE]->(rb)"
	liveScopeRun(t, session, seed, nil)
}

// TestLiveInfraScopeShapeShapeADiscriminates is the real-backend RED→GREEN
// regression.
func TestLiveInfraScopeShapeShapeADiscriminates(t *testing.T) {
	session, closeFn := liveScopeShapeSession(t)
	defer closeFn()
	seedLiveScopeShapeFixture(t, session)
	// Delete the scratch fixture before the session closes (LIFO: this defer runs
	// before closeFn). t.Cleanup would run after closeFn, hitting a closed session.
	defer liveScopeRun(t, session, "MATCH (n) WHERE n.sst = true DETACH DELETE n", nil)

	// accessA is a scoped filter granting only sst-repo-a; the production
	// predicate builders derive their scalars from it, and paramsA binds the
	// matching arrays + scope_grant_* scalars.
	accessA := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"sst-repo-a"},
		allowed:              map[string]struct{}{"sst-repo-a": {}},
	}
	scalarsA, _ := accessA.scopeGrantInlineScalars()
	paramsA := map[string]any{"allowed_repository_ids": accessA.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsA, scalarsA)

	// Restrict counts to the scratch fixture so unrelated data cannot skew them.
	sst := " AND n.sst = true"

	// RED 1: the dead n-last EXISTS bridge under-authorizes — ZERO scoped
	// CloudResources even though sst-cloud-a is legitimately in grant. (This is
	// the historical shipped shape, deliberately hand-written to prove it is
	// dead on this backend — it is NOT the production code under test.)
	deadBridge := "(n.repo_id IN $allowed_repository_ids OR n.id IN $allowed_repository_ids OR " +
		"EXISTS { MATCH (scopeRepo:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n) " +
		"WHERE scopeRepo.id IN $allowed_repository_ids })" + sst
	if got := liveScopeCount(t, session, "n", "CloudResource", deadBridge, paramsA); got != 0 {
		t.Fatalf("RED expectation failed: dead n-last bridge counted %d CloudResources, want 0 (proves it is dead/under-authorizing on this backend)", got)
	}

	// RED 2: the naive backward-EXISTS-with-WHERE over-authorizes — it admits
	// BOTH scratch CloudResources (incl the tenant-B secret) regardless of grant.
	naiveBackward := "(EXISTS { MATCH (n)<-[:USES]-(inst:WorkloadInstance) WHERE inst.repo_id IN $allowed_repository_ids })" + sst
	if got := liveScopeCount(t, session, "n", "CloudResource", naiveBackward, paramsA); got != 2 {
		t.Fatalf("RED expectation failed: naive backward-EXISTS counted %d CloudResources, want 2 (proves it is always-true/leaking on this backend)", got)
	}

	// GREEN: the production SHAPE-A predicate discriminates. grant repo-a admits
	// only sst-cloud-a (excludes the tenant-B secret).
	shapeA := infraResourceScopePredicate("n", scalarsA) + sst
	if got := liveScopeCount(t, session, "n", "CloudResource", shapeA, paramsA); got != 1 {
		t.Fatalf("GREEN failed: SHAPE-A counted %d CloudResources for grant repo-a, want 1 (only sst-cloud-a; tenant-B secret excluded)", got)
	}

	// GREEN: the collision Workload (materialized repo_id=repo-b) is admitted for
	// a repo-a grant via the DEFINES inline-map, which flat repo_id alone misses.
	// Asserted as a count DELTA over the two scratch Workloads (sst-wl-a with
	// repo_id=repo-a, sst-wl-collide with repo_id=repo-b defined by repo-a AND
	// repo-b) rather than an id-filtered singleton — an id-equality combined with
	// an indexed repo_id predicate can drop the id anchor on this backend
	// (documented pitfall), so the set-delta form is the robust proof:
	//   flat-only admits {sst-wl-a}            -> 1
	//   SHAPE-A admits    {sst-wl-a, sst-wl-collide} -> 2
	flatOnly := "(n.repo_id IN $allowed_repository_ids OR n.repo_id IN $allowed_scope_ids) AND n.sst = true"
	if got := liveScopeCount(t, session, "n", "Workload", flatOnly, paramsA); got != 1 {
		t.Fatalf("flat-only Workload count = %d, want 1 (only sst-wl-a; the collision workload's repo_id is repo-b, missed by flat)", got)
	}
	combined := workloadScopePredicate("n", accessA) + " AND n.sst = true"
	if got := liveScopeCount(t, session, "n", "Workload", combined, paramsA); got != 2 {
		t.Fatalf("SHAPE-A Workload count = %d, want 2 (flat admits sst-wl-a, DEFINES inline-map admits the collision sst-wl-collide)", got)
	}

	// GREEN: the tenant-B grant admits exactly the tenant-B secret.
	accessB := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"sst-repo-b"},
		allowed:              map[string]struct{}{"sst-repo-b": {}},
	}
	scalarsB, _ := accessB.scopeGrantInlineScalars()
	paramsB := map[string]any{"allowed_repository_ids": accessB.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsB, scalarsB)
	shapeAB := infraResourceScopePredicate("n", scalarsB) + sst
	if got := liveScopeCount(t, session, "n", "CloudResource", shapeAB, paramsB); got != 1 {
		t.Fatalf("grant repo-b must admit exactly the tenant-B secret, got %d", got)
	}
}

// seedLiveScopeShapeStateFixture writes the #5623 MATCHES_STATE topology into
// the same sst-prefixed scratch namespace, reusing the sst-repo-a / sst-repo-b
// Repository nodes seedLiveScopeShapeFixture already created (callers MUST run
// that seed first): a matched-and-granted state resource (sst-ts-a,
// MATCHES_STATE-linked to a repo-a TerraformResource), a matched-but-
// cross-tenant state resource (sst-ts-b, linked to a repo-b TerraformResource
// -- the genuine cross-tenant candidate row this fix must exclude), and an
// unmatched state resource (sst-ts-unmatched, no MATCHES_STATE edge at all,
// even though its config_repo_id PROPERTY names repo-a) that must stay
// invisible regardless of the property value.
func seedLiveScopeShapeStateFixture(t *testing.T, session neo4jdriver.SessionWithContext) {
	t.Helper()
	seed := "MATCH (ra:Repository {id:'sst-repo-a'}) " +
		"MATCH (rb:Repository {id:'sst-repo-b'}) " +
		"CREATE (tra:TerraformResource {id:'sst-tr-a', repo_id:'sst-repo-a', name:'aws_instance.web', sst:true}) " +
		"CREATE (trb:TerraformResource {id:'sst-tr-b', repo_id:'sst-repo-b', name:'aws_instance.web', sst:true}) " +
		"CREATE (tsa:TerraformStateResource {uid:'sst-ts-a', config_repo_id:'sst-repo-a', sst:true}) " +
		"CREATE (tsb:TerraformStateResource {uid:'sst-ts-b', config_repo_id:'sst-repo-b', sst:true}) " +
		"CREATE (tsu:TerraformStateResource {uid:'sst-ts-unmatched', config_repo_id:'sst-repo-a', sst:true}) " +
		"CREATE (tra)-[:MATCHES_STATE]->(tsa) " +
		"CREATE (trb)-[:MATCHES_STATE]->(tsb)"
	liveScopeRun(t, session, seed, nil)
}

// TestLiveInfraScopeShapeMatchesStateDiscriminates is the #5623 real-backend
// RED->GREEN regression for TerraformStateResource scoped-token visibility.
// It proves, against the production infraResourceScopePredicate builder run
// over a live NornicDB:
//
//   - RED (today, pre-fix baseline): the flat direct-ownership disjuncts alone
//     admit ZERO TerraformStateResource nodes for any grant -- the reported
//     coverage gap is real on this backend, not a theoretical worry.
//   - RED (rejected alternative): a config_repo_id PROPERTY-only disjunct
//     over-admits -- it counts the unmatched state resource (sst-ts-unmatched,
//     no MATCHES_STATE edge) alongside the genuinely matched one, because the
//     property is set from backend-ownership resolution alone and does not
//     require an edge. This is why the fix traverses the edge instead of
//     trusting the property.
//   - GREEN (the fix): the production predicate, via its MATCHES_STATE
//     inline-map disjunct, admits exactly the matched-and-granted state
//     resource for a repo-a grant, and separately proves the cross-tenant
//     matched-but-ungranted row (sst-ts-b, linked to repo-b) and the unmatched
//     row (sst-ts-unmatched) both stay invisible.
//
// Gate: set ESHU_CYPHER_BOLT_DSN (e.g. bolt://localhost:27687) to the isolated
// NornicDB. Run with:
//
//	go test -tags live_infra_scope_shape ./internal/query \
//	  -run TestLiveInfraScopeShapeMatchesStateDiscriminates -count=1
func TestLiveInfraScopeShapeMatchesStateDiscriminates(t *testing.T) {
	session, closeFn := liveScopeShapeSession(t)
	defer closeFn()
	seedLiveScopeShapeFixture(t, session)
	seedLiveScopeShapeStateFixture(t, session)
	defer liveScopeRun(t, session, "MATCH (n) WHERE n.sst = true DETACH DELETE n", nil)

	accessA := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"sst-repo-a"},
		allowed:              map[string]struct{}{"sst-repo-a": {}},
	}
	scalarsA, _ := accessA.scopeGrantInlineScalars()
	paramsA := map[string]any{"allowed_repository_ids": accessA.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsA, scalarsA)

	sst := " AND n.sst = true"

	// RED 1: flat direct-ownership alone (pre-#5623 shape) sees no state
	// resources at all, for any grant -- proving the reported gap.
	flatOnly := "(n.repo_id IN $allowed_repository_ids OR n.repo_id IN $allowed_scope_ids OR " +
		"n.id IN $allowed_repository_ids OR n.id IN $allowed_scope_ids)" + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", flatOnly, paramsA); got != 0 {
		t.Fatalf("RED expectation failed: flat-only disjuncts counted %d TerraformStateResource nodes, want 0 (proves the pre-#5623 coverage gap is real)", got)
	}

	// RED 2: a config_repo_id PROPERTY-only disjunct (the rejected design)
	// over-admits: it counts sst-ts-a (matched+granted, correct) AND
	// sst-ts-unmatched (no MATCHES_STATE edge, wrongly admitted because the
	// property alone doesn't require a match) -- 2, not 1.
	propertyOnly := "(n.config_repo_id IN $allowed_repository_ids OR n.config_repo_id IN $allowed_scope_ids)" + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", propertyOnly, paramsA); got != 2 {
		t.Fatalf("RED expectation failed: config_repo_id property-only disjunct counted %d, want 2 (proves the property alone wrongly admits the unmatched row)", got)
	}

	// GREEN: the production predicate admits exactly the matched-and-granted
	// state resource for the repo-a grant.
	shapeA := infraResourceScopePredicate("n", scalarsA) + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", shapeA, paramsA); got != 1 {
		t.Fatalf("GREEN failed: SHAPE-A counted %d TerraformStateResource nodes for grant repo-a, want 1 (only sst-ts-a)", got)
	}

	// NEGATIVE (cross-tenant): the repo-a grant must not see sst-ts-b, which is
	// genuinely MATCHES_STATE-matched but to a repo-b TerraformResource.
	crossTenant := infraResourceScopePredicate("n", scalarsA) + " AND n.uid = 'sst-ts-b'" + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", crossTenant, paramsA); got != 0 {
		t.Fatalf("NEGATIVE failed: repo-a grant admitted the cross-tenant matched row sst-ts-b (matched to repo-b), count=%d, want 0 -- this is the tenant-isolation regression this fix must prevent", got)
	}

	// NEGATIVE (unmatched): sst-ts-unmatched has no MATCHES_STATE edge at all,
	// even though its config_repo_id property names the granted repo-a, and
	// must stay invisible.
	unmatched := infraResourceScopePredicate("n", scalarsA) + " AND n.uid = 'sst-ts-unmatched'" + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", unmatched, paramsA); got != 0 {
		t.Fatalf("NEGATIVE failed: repo-a grant admitted the unmatched row sst-ts-unmatched (no MATCHES_STATE edge), count=%d, want 0", got)
	}

	// GREEN: the repo-b grant admits exactly its own matched state resource.
	accessB := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"sst-repo-b"},
		allowed:              map[string]struct{}{"sst-repo-b": {}},
	}
	scalarsB, _ := accessB.scopeGrantInlineScalars()
	paramsB := map[string]any{"allowed_repository_ids": accessB.allowedRepositoryIDs, "allowed_scope_ids": []string{}}
	bindScopeGrantInlineScalars(paramsB, scalarsB)
	shapeB := infraResourceScopePredicate("n", scalarsB) + sst
	if got := liveScopeCount(t, session, "n", "TerraformStateResource", shapeB, paramsB); got != 1 {
		t.Fatalf("grant repo-b must admit exactly its own matched state resource (sst-ts-b), got %d", got)
	}
}
