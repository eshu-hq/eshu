// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryselector

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeRepoIdentityGraphQuery captures the Cypher text and params
// HydrateResolvedEntityRepoIdentity's workload backfill query sends.
type fakeRepoIdentityGraphQuery struct {
	gotCypher string
	gotParams map[string]any
	rows      []map[string]any
}

func (f *fakeRepoIdentityGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.gotCypher = cypher
	f.gotParams = params
	return f.rows, nil
}

func (f *fakeRepoIdentityGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestHydrateResolvedEntityRepoIdentityPinsCypherAndSplicesAccessPredicate
// pins the workload-backfill statement's shape (UNWIND/MATCH/OPTIONAL MATCH
// on Repository-[:DEFINES]->Workload, direct and via-instance) and proves
// the caller's scoped access filter is spliced into both branches, each as
// GraphWhereClause's own WHERE. Losing either splice would let a scoped
// caller's workload backfill read another tenant's repository identity.
//
// #6786 review follow-up (F2): the UNWIND loop variable is `requested_id`,
// not `entity_id` -- the retired name collided with the RETURN column alias
// below it and made NornicDB v1.3.3 return garbage for both (proven live;
// see entity_repo_identity.go's doc comment on this query). The direct-
// DEFINES branch anchors on `e` directly (`(repo)-[:DEFINES]->(e)`) rather
// than a separate `direct` variable plus a `WHERE direct = e` node-equality
// comparison, which is the second, independently-proven-safe half of that
// fix.
func TestHydrateResolvedEntityRepoIdentityPinsCypherAndSplicesAccessPredicate(t *testing.T) {
	t.Parallel()

	graph := &fakeRepoIdentityGraphQuery{
		rows: []map[string]any{
			{"entity_id": "workload:1", "repo_id": "repo-1", "repo_name": "repo-one"},
		},
	}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-1"},
	})
	entity := map[string]any{
		"id":     "workload:1",
		"labels": []string{"Workload"},
	}

	if _, err := HydrateResolvedEntityRepoIdentity(ctx, graph, nil, []map[string]any{entity}); err != nil {
		t.Fatalf("HydrateResolvedEntityRepoIdentity() error = %v, want nil", err)
	}

	for _, want := range []string{
		"UNWIND $entity_ids AS requested_id",
		"MATCH (e) WHERE e.id = requested_id",
		"OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(e)",
		"OPTIONAL MATCH (repoViaInstance:Repository)-[:DEFINES]->(instanceWorkload:Workload)<-[:INSTANCE_OF]-(e)",
		"RETURN e.id AS entity_id,",
		"coalesce(repo.id, repoViaInstance.id) AS repo_id",
		"coalesce(repo.name, repoViaInstance.name) AS repo_name",
	} {
		if !strings.Contains(graph.gotCypher, want) {
			t.Fatalf("cypher = %q, want it to contain %q", graph.gotCypher, want)
		}
	}
	if strings.Contains(graph.gotCypher, "direct") {
		t.Fatalf("cypher = %q, want the retired `direct` variable/comparison gone", graph.gotCypher)
	}

	wantRepoWhere := "WHERE (repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)"
	if !strings.Contains(graph.gotCypher, wantRepoWhere) {
		t.Fatalf("cypher = %q, want the direct-DEFINES branch to carry %q", graph.gotCypher, wantRepoWhere)
	}
	wantViaInstanceWhere := "WHERE (repoViaInstance.id IN $allowed_repository_ids OR repoViaInstance.id IN $allowed_scope_ids)"
	if !strings.Contains(graph.gotCypher, wantViaInstanceWhere) {
		t.Fatalf("cypher = %q, want the via-instance branch to carry %q", graph.gotCypher, wantViaInstanceWhere)
	}
	querytestutil.AssertCypherHasNoBrokenAndOr(t, graph.gotCypher)

	allowedRepoIDs, ok := graph.gotParams["allowed_repository_ids"].([]string)
	if !ok || len(allowedRepoIDs) != 1 || allowedRepoIDs[0] != "repo-1" {
		t.Fatalf("params[allowed_repository_ids] = %#v, want [repo-1]", graph.gotParams["allowed_repository_ids"])
	}

	if got, want := entity["repo_id"], "repo-1"; got != want {
		t.Fatalf("entity[repo_id] = %#v, want %#v", got, want)
	}
}

// TestHydrateResolvedEntityRepoIdentityDropsUngrantedHydratedRepo is the
// #6786 review follow-up (R2-3): the hydration query's own
// `OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(e) WHERE (grant)` is a
// backward `-[:DEFINES]->` pattern with an inner WHERE, the same shape class
// F1 (workload_context.go) stopped trusting alone. This test simulates that
// WHERE failing to filter: the fake returns a row naming an ungranted
// repository, as if the backend's WHERE had not applied. Go must still
// refuse to attach it.
func TestHydrateResolvedEntityRepoIdentityDropsUngrantedHydratedRepo(t *testing.T) {
	t.Parallel()

	graph := &fakeRepoIdentityGraphQuery{
		rows: []map[string]any{
			// The backend's WHERE should have excluded repo-2 (only repo-1
			// is granted below), but this fake simulates it not doing so.
			{"entity_id": "workload:1", "repo_id": "repo-2", "repo_name": "ungranted-repo"},
		},
	}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-1"},
	})
	entity := map[string]any{
		"id":     "workload:1",
		"labels": []string{"Workload"},
	}

	if _, err := HydrateResolvedEntityRepoIdentity(ctx, graph, nil, []map[string]any{entity}); err != nil {
		t.Fatalf("HydrateResolvedEntityRepoIdentity() error = %v, want nil", err)
	}

	if got := EntityString(entity, "repo_id"); got != "" {
		t.Fatalf("entity[repo_id] = %q, want empty: an ungranted hydrated repo must never be attached even if the backend's own WHERE failed to filter it", got)
	}
	if got := EntityString(entity, "repo_name"); got != "" {
		t.Fatalf("entity[repo_name] = %q, want empty alongside the dropped repo_id", got)
	}
}

// TestHydrateResolvedEntityRepoIdentityScrubsProjectionPlaceholder is the
// #6408 regression: a backend that leaks its own unresolved projection
// expression (for example "coalesce(repo.id, repoViaInstance.id)") as a
// literal repo_id/repo_name value must have that value cleared before
// anything else runs, never surfaced to a caller as if it were a real
// repository identity.
func TestHydrateResolvedEntityRepoIdentityScrubsProjectionPlaceholder(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"id":        "content-entity:leaked",
		"labels":    []string{"Function"},
		"repo_id":   "coalesce(repo.id, repoViaInstance.id)",
		"repo_name": "repo.name",
	}

	// A nil graph and nil content mean nothing can backfill a real value, so
	// this isolates the scrub: if it ran, the placeholder is gone and stays
	// empty; if it did not, the leaked expression text would still be there.
	if _, err := HydrateResolvedEntityRepoIdentity(context.Background(), nil, nil, []map[string]any{entity}); err != nil {
		t.Fatalf("HydrateResolvedEntityRepoIdentity() error = %v, want nil", err)
	}

	if got := entity["repo_id"]; got != "" {
		t.Fatalf("entity[repo_id] = %#v, want scrubbed to empty", got)
	}
	if got := entity["repo_name"]; got != "" {
		t.Fatalf("entity[repo_name] = %#v, want scrubbed to empty", got)
	}
}

// TestHydrateResolvedEntityRepoIdentityRepositoryEntitySelfIdentifies proves
// a Repository-labeled entity takes its own id/name as repo_id/repo_name
// rather than reading the (nonexistent) repo_id/repo_name properties off
// itself.
func TestHydrateResolvedEntityRepoIdentityRepositoryEntitySelfIdentifies(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"id":     "repo-self",
		"name":   "self-named-repo",
		"labels": []string{"Repository"},
	}

	if _, err := HydrateResolvedEntityRepoIdentity(context.Background(), nil, nil, []map[string]any{entity}); err != nil {
		t.Fatalf("HydrateResolvedEntityRepoIdentity() error = %v, want nil", err)
	}

	if got, want := entity["repo_id"], "repo-self"; got != want {
		t.Fatalf("entity[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := entity["repo_name"], "self-named-repo"; got != want {
		t.Fatalf("entity[repo_name] = %#v, want %#v", got, want)
	}
}

var _ querycontract.GraphQuery = (*fakeRepoIdentityGraphQuery)(nil)
