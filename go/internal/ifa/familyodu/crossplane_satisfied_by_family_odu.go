// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// CrossplaneSatisfiedByFamilyOduName is this Odù's catalog name, the ref
	// a materialized_edges:crossplane_satisfied_by coverage row would name
	// to resolve through it.
	CrossplaneSatisfiedByFamilyOduName = "odu:ifa-crossplane-satisfied-by-family"

	// crossplaneSatisfiedByFamilyClaimsScopeID is the git scope carrying the
	// Claim-candidate facts. Claims and XRDs commonly live in separate repos
	// — an app repo's claim resolves against a platform repo's XRD — so the
	// Odù keeps them in two scopes, mirroring the handler's own-scope plus
	// cross-scope XRD load rather than a single-scope generation no
	// production intent ever carries for this family.
	crossplaneSatisfiedByFamilyClaimsScopeID = "git:eshu-fixture-crossplane-claims"

	// crossplaneSatisfiedByFamilyPlatformScopeID is the git scope carrying
	// the CrossplaneXRD facts.
	crossplaneSatisfiedByFamilyPlatformScopeID = "git:eshu-fixture-crossplane-platform"

	// crossplaneSatisfiedByFamilyGenerationID is the single generation both
	// scopes replay.
	crossplaneSatisfiedByFamilyGenerationID = "gen-ifa-crossplane-satisfied-by-family-1"

	// crossplaneSatisfiedByFamilyCollectorKind mirrors what the git
	// collector stamps on a content_entity fact.
	crossplaneSatisfiedByFamilyCollectorKind = "git"
)

// crossplaneSatisfiedByFixture describes one content_entity fact in the Odù.
type crossplaneSatisfiedByFixture struct {
	// EntityID is the node's canonical uid. It is the edge endpoint
	// identity, so it is what the expected-edge fixture's source and target
	// identities are derived from.
	EntityID string
	// EntityType is the canonical Neo4j label materializeEntities stamps:
	// "K8sResource" for a Claim candidate, "CrossplaneXRD" for an XRD.
	EntityType string
	// RepoID and RelativePath mirror the collector's content_entity
	// payload: which repo file produced this entity.
	RepoID string
	// RelativePath is the repo file that produced this entity.
	RelativePath string
	// EntityName is the Kubernetes object name, carried for collector-shape
	// fidelity (the extractor never reads it).
	EntityName string
	// Metadata is the entity_metadata map the extractor joins on:
	// api_version/kind for a Claim candidate, group/claim_kind for an XRD.
	Metadata map[string]any
	// PlatformScope selects the XRD scope; claims always use the claims
	// scope.
	PlatformScope bool
	// Tombstone marks a retired fact the extractor must skip.
	Tombstone bool
}

// crossplaneSatisfiedByFamilyFixtures is the hand-authored content_entity
// set this Odù carries: two edge-producing claims against one XRD and five
// deliberate non-producers covering the ambiguous-match, zero-match,
// core-group, cluster-scoped-XRD and tombstone skip branches.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates a representative pick for an ambiguous match, never promotes
// an ordinary object with no XRD to a Claim, and never matches on an empty
// group or claim kind — and the writer's two MATCH clauses would no-op on a
// missing node anyway. Without them, a regression that resolved every
// K8sResource against the first XRD in the index would still reproduce the
// expected set exactly and this fixture would report green.
var crossplaneSatisfiedByFamilyFixtures = []crossplaneSatisfiedByFixture{
	{
		// XRD both edge-producing claims resolve against.
		EntityID:     "xrd:postgresql",
		EntityType:   "CrossplaneXRD",
		RepoID:       "repo-platform",
		RelativePath: "xrds/postgresql.yaml",
		EntityName:   "postgresql",
		Metadata: map[string]any{
			"group":      "database.example.com",
			"kind":       "XPostgreSQL",
			"claim_kind": "PostgreSQL",
		},
		PlatformScope: true,
	},
	{
		// First half of the ambiguity pair: shares (group, claim_kind)
		// with xrd:redis-b.
		EntityID:     "xrd:redis-a",
		EntityType:   "CrossplaneXRD",
		RepoID:       "repo-platform",
		RelativePath: "xrds/redis-a.yaml",
		EntityName:   "redis-a",
		Metadata: map[string]any{
			"group":      "cache.example.com",
			"kind":       "XRedis",
			"claim_kind": "Redis",
		},
		PlatformScope: true,
	},
	{
		// Second half of the ambiguity pair.
		EntityID:     "xrd:redis-b",
		EntityType:   "CrossplaneXRD",
		RepoID:       "repo-platform",
		RelativePath: "xrds/redis-b.yaml",
		EntityName:   "redis-b",
		Metadata: map[string]any{
			"group":      "cache.example.com",
			"kind":       "XRedis",
			"claim_kind": "Redis",
		},
		PlatformScope: true,
	},
	{
		// NO EDGE: a cluster-scoped-only XRD carries no claim_kind, so it
		// never enters the join index. A claim naming its group alone must
		// not resolve against it.
		EntityID:     "xrd:cluster-scoped",
		EntityType:   "CrossplaneXRD",
		RepoID:       "repo-platform",
		RelativePath: "xrds/cluster-scoped.yaml",
		EntityName:   "cluster-scoped",
		Metadata: map[string]any{
			"group": "database.example.com",
			"kind":  "XClusterScoped",
		},
		PlatformScope: true,
	},
	{
		// EDGE: (database.example.com, PostgreSQL) matches exactly the one
		// XRD above.
		EntityID:     "k8s:claim-orders-db",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "claims/orders-db.yaml",
		EntityName:   "orders-db",
		Metadata: map[string]any{
			"api_version": "database.example.com/v1",
			"kind":        "PostgreSQL",
		},
	},
	{
		// EDGE: a different apiVersion segment (v1alpha1) in the same group
		// still joins on the group segment alone, and the second claim
		// against the same XRD proves the fan-out counts rather than
		// collapsing to one edge.
		EntityID:     "k8s:claim-billing-db",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "claims/billing-db.yaml",
		EntityName:   "billing-db",
		Metadata: map[string]any{
			"api_version": "database.example.com/v1alpha1",
			"kind":        "PostgreSQL",
		},
	},
	{
		// NO EDGE: (cache.example.com, Redis) matches two XRDs. The
		// extractor must count the ambiguity and write nothing, never a
		// fabricated representative pick.
		EntityID:     "k8s:claim-cache",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "claims/cache.yaml",
		EntityName:   "cache",
		Metadata: map[string]any{
			"api_version": "cache.example.com/v1",
			"kind":        "Redis",
		},
	},
	{
		// NO EDGE: an ordinary Kubernetes object with no XRD in its group
		// is not a Claim and produces no edge and no tally entry.
		EntityID:     "k8s:deploy-orders-api",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "apps/orders-api.yaml",
		EntityName:   "orders-api",
		Metadata: map[string]any{
			"api_version": "apps/v1",
			"kind":        "Deployment",
		},
	},
	{
		// NO EDGE: a core-group apiVersion carries no "/" so its group is
		// empty, and an empty group never equals a non-empty XRD group.
		EntityID:     "k8s:config-app",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "apps/app-config.yaml",
		EntityName:   "app-config",
		Metadata: map[string]any{
			"api_version": "v1",
			"kind":        "ConfigMap",
		},
	},
	{
		// NO EDGE: a tombstoned claim that would otherwise match the
		// postgresql XRD. Tombstones never resurrect edges.
		EntityID:     "k8s:claim-retired-db",
		EntityType:   "K8sResource",
		RepoID:       "repo-claims-app",
		RelativePath: "claims/retired-db.yaml",
		EntityName:   "retired-db",
		Metadata: map[string]any{
			"api_version": "database.example.com/v1",
			"kind":        "PostgreSQL",
		},
		Tombstone: true,
	},
}

// CrossplaneSatisfiedByFamilyOdu builds the cataloged Odù for the
// crossplane_satisfied_by direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It carries no encode
// step — content_entity payloads are collector-shaped maps, not SDK-typed
// values — so there is nothing to panic on; the guard's exact-set
// comparison is what voids stale claims here.
func CrossplaneSatisfiedByFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(crossplaneSatisfiedByFamilyFixtures))
	for _, fixture := range crossplaneSatisfiedByFamilyFixtures {
		scopeID := crossplaneSatisfiedByFamilyClaimsScopeID
		if fixture.PlatformScope {
			scopeID = crossplaneSatisfiedByFamilyPlatformScopeID
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          scopeID,
			GenerationID:     crossplaneSatisfiedByFamilyGenerationID,
			FactKind:         ContentEntityFactKind,
			StableFactKey:    "content_entity:" + fixture.EntityID,
			CollectorKind:    crossplaneSatisfiedByFamilyCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			IsTombstone:      fixture.Tombstone,
			Payload: map[string]any{
				"graph_id":        fixture.EntityID,
				"graph_kind":      "content_entity",
				"entity_id":       fixture.EntityID,
				"repo_id":         fixture.RepoID,
				"relative_path":   fixture.RelativePath,
				"entity_type":     fixture.EntityType,
				"entity_name":     fixture.EntityName,
				"entity_metadata": fixture.Metadata,
			},
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: CrossplaneSatisfiedByFamilyOduName, Facts: factsForOdu},
		Detail: "ten content_entity facts for the direct-materialization crossplane_satisfied_by family: one XRD with two edge-producing claims (proving the fan-out counts) and five deliberate non-producers covering the ambiguous-match, zero-match, core-group, cluster-scoped-XRD and tombstone branches, so the SATISFIED_BY expected set proves both promotion and restraint",
	}
}
