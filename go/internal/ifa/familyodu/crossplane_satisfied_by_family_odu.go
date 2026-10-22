// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
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

	// Generations are PER SCOPE, not shared: the drive/projector path keys
	// generations without a scope qualifier, so one generation ID replayed
	// in two scopes collides and the loser fails with "generation not
	// found" (proven live). The cloud_resource sibling uses the same
	// per-scope suffix shape (...-aws-1, ...-gcp-1, ...-azure-1).
	crossplaneSatisfiedByFamilyClaimsGenerationID = "gen-ifa-crossplane-satisfied-by-family-claims-1"

	// crossplaneSatisfiedByFamilyPlatformGenerationID is the platform XRD
	// scope's generation. The handler's cross-scope XRD load only sees
	// platform facts once THIS generation is active, so the live cells
	// drive both scopes and drain to the residual bound rather than
	// assuming the platform side is ready when the claims intent claims.
	crossplaneSatisfiedByFamilyPlatformGenerationID = "gen-ifa-crossplane-satisfied-by-family-platform-1"

	// crossplaneSatisfiedByFamilyCollectorKind mirrors what the git
	// collector stamps on a content_entity fact.
	crossplaneSatisfiedByFamilyCollectorKind = "git"

	// crossplaneSatisfiedByFamilySchemaVersion is the content_entity wire
	// version these facts carry. The drive path rejects a cassette fact
	// with an empty schema_version, so the compiled half must stamp what
	// the cassette carries (and the lockstep test holds them equal) --
	// the same 1.0.0 the code_interproc sibling stamps.
	crossplaneSatisfiedByFamilySchemaVersion = "1.0.0"
)

// crossplaneSatisfiedByFixture describes one content_entity fact in the Odù.
type crossplaneSatisfiedByFixture struct {
	// EntityID is the node's canonical uid for Claim candidates: it is the
	// edge source identity, so it is what the expected-edge fixture's
	// source identities are derived from. For XRD fixtures this input is
	// schematic only (xrd: names): the builder replaces it with
	// content.CanonicalEntityID because the canonical projector recomputes
	// CrossplaneXRD uids, and the writer's MATCH needs the fact to carry
	// the recomputed id. The expected-edge target identities derive from
	// the COMPUTED ids, not these inputs.
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
// materializededges' guard test resolves it by name. Content_entity payloads
// are collector-shaped maps with no encode step; the TYPED file and
// repository facts below DO encode through factschema and panic on failure
// (a fixture that cannot encode is a broken fixture, and the guard's
// exact-set comparison is what voids stale claims here).
//
// The Odù carries the full repo snapshot the live projector needs, not just
// the entities the extractor reads: two repository facts (one per repo),
// one TYPED file fact per relative_path the fixtures reference, and the ten
// content_entity facts. The entities phase writes nodes with containment
// (MATCH File first), so file facts are load-bearing: without them the
// entity statements complete having written nothing, and the writer's MATCH
// misses every endpoint. The handler's fact load filters content_entity
// only, so the repo/file facts are invisible to extraction.
func CrossplaneSatisfiedByFamilyOdu() CatalogOdu {
	// Scope-grouped fact order (platform repo, platform files, platform
	// entities, then claims repo, claims files, claims entities) mirrors the
	// cassette's scope nesting exactly: the lockstep test compares canonical
	// JSON arrays in order, and a flat repos-then-files-then-entities layout
	// would diverge from the renderer's scope-grouped projection on every
	// run. Fact ORDER carries no semantic meaning (the projector keys by
	// scope+generation+stable key); the grouping exists only to keep the
	// two halves byte-comparable.
	factsForOdu := make([]facts.Envelope, 0, len(crossplaneSatisfiedByFamilyFixtures)+12)
	for _, scope := range []struct {
		scopeID      string
		generationID string
		repoID       string
		localPath    string
		platform     bool
	}{
		{
			scopeID:      crossplaneSatisfiedByFamilyPlatformScopeID,
			generationID: crossplaneSatisfiedByFamilyPlatformGenerationID,
			repoID:       "repo-platform",
			localPath:    "/repo-crossplane-platform",
			platform:     true,
		},
		{
			scopeID:      crossplaneSatisfiedByFamilyClaimsScopeID,
			generationID: crossplaneSatisfiedByFamilyClaimsGenerationID,
			repoID:       "repo-claims-app",
			localPath:    "/repo-claims-app",
			platform:     false,
		},
	} {
		factsForOdu = append(factsForOdu, crossplaneSatisfiedByRepositoryFact(
			scope.scopeID, scope.generationID, scope.repoID, scope.localPath,
		))
		seenFiles := map[string]bool{}
		for _, fixture := range crossplaneSatisfiedByFamilyFixtures {
			if fixture.PlatformScope != scope.platform || seenFiles[fixture.RelativePath] {
				continue
			}
			seenFiles[fixture.RelativePath] = true
			factsForOdu = append(factsForOdu, crossplaneSatisfiedByFileFact(
				scope.scopeID, scope.generationID, fixture.RepoID, fixture.RelativePath,
			))
		}
		for _, fixture := range crossplaneSatisfiedByFamilyFixtures {
			if fixture.PlatformScope != scope.platform {
				continue
			}
			factsForOdu = append(factsForOdu, crossplaneSatisfiedByContentEntityFact(
				scope.scopeID, scope.generationID, fixture,
			))
		}
	}
	return crossplaneSatisfiedByAssembleOdu(factsForOdu)
}

func crossplaneSatisfiedByAssembleOdu(factsForOdu []facts.Envelope) CatalogOdu {
	return CatalogOdu{
		Odu:    Odu{Name: CrossplaneSatisfiedByFamilyOduName, Facts: factsForOdu},
		Detail: "twenty-two facts for the direct-materialization crossplane_satisfied_by family: two repository facts, ten typed file facts (one per referenced path), and ten content_entity facts -- one XRD with two edge-producing claims (proving the fan-out counts) and five deliberate non-producers covering the ambiguous-match, zero-match, core-group, cluster-scoped-XRD and tombstone branches, so the SATISFIED_BY expected set proves both promotion and restraint",
	}
}

func crossplaneSatisfiedByContentEntityFact(scopeID, generationID string, fixture crossplaneSatisfiedByFixture) facts.Envelope {
	entityID := fixture.EntityID
	if fixture.PlatformScope {
		// The canonical projector RECOMPUTES CrossplaneXRD node uids
		// (CrossplaneXRD is in canonicalNamePathLineEntityLabels), so
		// the fact's entity_id must already be that recomputed id or
		// the writer's MATCH (xrd {uid: row.xrd_uid}) misses nodes the
		// projection did commit. Compute it exactly as production
		// facts carry it, not as a pretty xrd: name. K8sResource keeps
		// the incoming id (it is not in the recompute set), so claims
		// keep their fixture names.
		entityID = content.CanonicalEntityID(fixture.RepoID, fixture.RelativePath, "CrossplaneXRD", fixture.EntityName, 0)
	}
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         ContentEntityFactKind,
		StableFactKey:    "content_entity:" + entityID,
		SchemaVersion:    crossplaneSatisfiedByFamilySchemaVersion,
		CollectorKind:    crossplaneSatisfiedByFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		IsTombstone:      fixture.Tombstone,
		Payload: map[string]any{
			"graph_id":        entityID,
			"graph_kind":      "content_entity",
			"entity_id":       entityID,
			"repo_id":         fixture.RepoID,
			"relative_path":   fixture.RelativePath,
			"entity_type":     fixture.EntityType,
			"entity_name":     fixture.EntityName,
			"entity_metadata": fixture.Metadata,
		},
	}
}

// crossplaneSatisfiedByRepositoryFact builds one typed repository fact for
// the given repo. Repository nodes are what the entities phase hangs File
// nodes off (MATCH Repository first), so without these the file statements
// complete having written nothing, exactly like the entity statements did
// without files.
func crossplaneSatisfiedByRepositoryFact(scopeID, generationID, repoID, localPath string) facts.Envelope {
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         RepositoryFactKind,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "repository:" + repoID,
		CollectorKind:    crossplaneSatisfiedByFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"repo_id":       repoID,
			"source_run_id": "run-ifa-crossplane-satisfied-by-family-1",
			"local_path":    localPath,
		},
	}
}

// crossplaneSatisfiedByFileFact builds one TYPED file fact for a referenced
// path. The entities phase writes nodes with containment (MATCH File
// first), so every relative_path the content_entity fixtures reference
// needs its file fact or the entity statements complete having written
// nothing. ParsedFileData stays empty: the File node write needs path and
// repo identity, not parsed content.
func crossplaneSatisfiedByFileFact(scopeID, generationID, repoID, relativePath string) facts.Envelope {
	graphID := repoID + ":" + relativePath
	graphKind := "file"
	isDependency := false
	language := "yaml"
	payload, err := factschema.EncodeCodegraphFile(codegraphv1.File{
		RepoID:         repoID,
		RelativePath:   relativePath,
		ParsedFileData: map[string]any{},
		GraphID:        &graphID,
		GraphKind:      &graphKind,
		Language:       &language,
		IsDependency:   &isDependency,
	})
	if err != nil {
		panic(fmt.Sprintf("familyodu: encode crossplane file %q: %v", relativePath, err))
	}
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         FileFactKind,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "file:" + repoID + ":" + relativePath,
		CollectorKind:    crossplaneSatisfiedByFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload:          payload,
	}
}
