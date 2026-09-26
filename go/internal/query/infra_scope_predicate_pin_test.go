// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestInfraResourceScopeListPredicatePinsAuthorizationText pins the text of the
// Neo4j list-EXISTS grant predicate (#7215). It is the authorization predicate
// for scoped tokens on Neo4j; weakening any EXISTS family (for example to
// WHERE true) is a whole-tenant leak, and the only other test that would catch
// it is the scheduled live lane. Each family must keep its edge type,
// direction, anchor label, tested property and grant list exactly.
func TestInfraResourceScopeListPredicatePinsAuthorizationText(t *testing.T) {
	t.Parallel()

	for _, alias := range []string{"n", "target", "source"} {
		alias := alias
		t.Run(alias, func(t *testing.T) {
			t.Parallel()
			got := infraResourceScopeListPredicate(alias)

			// The four direct-ownership disjuncts, then the four EXISTS
			// families, in order, joined by OR and wrapped in one group.
			ordered := []string{
				alias + ".repo_id IN $allowed_repository_ids",
				alias + ".repo_id IN $allowed_scope_ids",
				alias + ".id IN $allowed_repository_ids",
				alias + ".id IN $allowed_scope_ids",
				// USES: backward, WorkloadInstance, repo_id, grant list.
				"EXISTS { MATCH (" + alias + ")<-[:USES]-(scopeUsesInstance:WorkloadInstance) " +
					"WHERE scopeUsesInstance.repo_id IN $scope_grants }",
				// MATCHES_STATE: backward, TerraformResource, repo_id, grant list.
				"EXISTS { MATCH (" + alias + ")<-[:MATCHES_STATE]-(scopeStateConfig:TerraformResource) " +
					"WHERE scopeStateConfig.repo_id IN $scope_grants }",
				// DEPLOYMENT_SOURCE: forward, Repository, id, both allowed arrays.
				"EXISTS { MATCH (" + alias + ")-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository) " +
					"WHERE (scopeDeployRepo.id IN $allowed_repository_ids OR scopeDeployRepo.id IN $allowed_scope_ids) }",
				// DEFINES: backward, Repository, id, grant list.
				"EXISTS { MATCH (" + alias + ")<-[:DEFINES]-(scopeDefiningRepo:Repository) " +
					"WHERE scopeDefiningRepo.id IN $scope_grants }",
			}
			want := "(" + strings.Join(ordered, " OR ") + ")"
			if got != want {
				t.Fatalf("predicate text drifted:\n got: %s\nwant: %s", got, want)
			}

			// Independent structural assertions so a failure names the family.
			if strings.Contains(got, "WHERE true") || strings.Contains(strings.ToLower(got), "true") {
				t.Fatalf("predicate must never contain a constant-true term:\n%s", got)
			}
			if n := strings.Count(got, "EXISTS {"); n != 4 {
				t.Fatalf("EXISTS families = %d, want 4:\n%s", n, got)
			}
			if n := strings.Count(got, "IN $scope_grants"); n != 3 {
				t.Fatalf("`IN $scope_grants` terms = %d, want 3 (USES, MATCHES_STATE, DEFINES):\n%s", n, got)
			}
			for _, must := range []string{
				"(" + alias + ")<-[:USES]-(scopeUsesInstance:WorkloadInstance)",
				"(" + alias + ")<-[:MATCHES_STATE]-(scopeStateConfig:TerraformResource)",
				"(" + alias + ")-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository)",
				"(" + alias + ")<-[:DEFINES]-(scopeDefiningRepo:Repository)",
			} {
				if !strings.Contains(got, must) {
					t.Fatalf("missing anchored pattern %q:\n%s", must, got)
				}
			}
		})
	}
}
