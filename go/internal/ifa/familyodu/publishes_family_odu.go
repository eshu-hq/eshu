// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

const (
	// PublishesFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:publishes coverage row would name to resolve
	// through it.
	PublishesFamilyOduName = "odu:ifa-publishes-family"

	// publishesFamilyScopeID is the single scope every fact in this Odù
	// belongs to: the package-registry scope whose correlation intent
	// writes. The repository facts ride the handler's cross-scope
	// repository load; the builders themselves never filter by scope, so
	// the single-scope flattening is behavior-preserving for the offline
	// proof. The live cell must drive the writing intent in this scope,
	// or update the fixture's identity pins to the scope it uses — the
	// guard test reds on a mismatch either way.
	publishesFamilyScopeID = "package-registry:npm:eshu-fixture-team-api"

	// publishesFamilyGenerationID is the one scope generation the Odù
	// replays.
	publishesFamilyGenerationID = "gen-ifa-publishes-family-1"

	// publishesFamilySourceRunID is the collector run the repository facts
	// attribute themselves to. Canonical projection requires it on every
	// repository fact (identity without it drops the row): it mirrors the
	// run-ifa-<family>-family-1 shape the git collector emits, not a stable
	// content hash.
	publishesFamilySourceRunID = "run-ifa-publishes-family-1"

	// publishesFamilyRepositoryID is the repository that owns the
	// fixture package: every edge-producing hint resolves to it by exact
	// remote-URL match.
	publishesFamilyRepositoryID = "repository:eshu-fixture-team-api"

	// publishesFamilyRemoteURL is that repository's remote.
	publishesFamilyRemoteURL = "https://github.com/acme/team-api"

	// publishesFamilyPackageID is the owned package: the Package edge
	// target.
	publishesFamilyPackageID = "pkg:npm:@acme/team-api"

	// publishesFamilySchemaVersion, publishesFamilyRegistryCollectorKind
	// and publishesFamilyGitCollectorKind mirror what the collectors
	// stamp on package-registry and repository facts.
	publishesFamilySchemaVersion         = "1.0.0"
	publishesFamilyRegistryCollectorKind = "package_registry"
	publishesFamilyGitCollectorKind      = "git"
)

// publishesFamilyHint describes one package_registry.source_hint fact in
// the Odù.
type publishesFamilyHint struct {
	// PackageID is the hinted package.
	PackageID string
	// VersionID scopes the hint to one published version. Empty means
	// package-level: it classifies ownership for the package node and
	// pairs with every version fact of that package on the publication
	// path.
	VersionID string
	// HintKind is the registry's hint classification. Only "repository"
	// can prove ownership; anything else is provenance-only.
	HintKind string
	// URL is the hinted source URL, carried as both normalized and raw.
	URL string
}

// publishesFamilyHints holds the hint half of the fixture: three
// edge-producing hints (package-level exact, version-scoped exact, and the
// package-level exact that also feeds the publication path) and four
// deliberate non-producers covering the stale, rejected,
// unresolved and ambiguous branches on both the ownership and publication
// paths.
var publishesFamilyHints = []publishesFamilyHint{
	// EDGE (ownership, Package target) + EDGE (publication, PackageVersion
	// target via ver-team-api-1.0.0): exact match on the owning remote.
	{PackageID: publishesFamilyPackageID, HintKind: "repository", URL: publishesFamilyRemoteURL},
	// EDGE (ownership, PackageVersion target): a version-scoped exact
	// match. It pairs with no version fact, so it reaches no publication
	// decision — ownership alone proves the version-targeted write path.
	{PackageID: publishesFamilyPackageID, VersionID: "ver-team-api-2.0.0", HintKind: "repository", URL: publishesFamilyRemoteURL},
	// NO EDGE either path: the only remote match is tombstoned. The
	// ownership decision goes stale and the version's publication
	// decision goes stale with it.
	{PackageID: "pkg:npm:@acme/stale-api", HintKind: "repository", URL: "https://github.com/acme/stale-api"},
	// NO EDGE either path: a homepage hint is provenance-only and can
	// never prove ownership, on whichever path pairs it.
	{PackageID: publishesFamilyPackageID, HintKind: "homepage", URL: publishesFamilyRemoteURL},
	// NO EDGE either path: no repository remote matches.
	{PackageID: publishesFamilyPackageID, HintKind: "repository", URL: "https://github.com/acme/nonexistent"},
	// NO EDGE: two active repositories share this remote, so the hint is
	// ambiguous and neither is picked. It names a package with no version
	// facts, so the publication path never pairs it either.
	{PackageID: "pkg:npm:@acme/shared-lib", HintKind: "repository", URL: "https://github.com/acme/shared-lib"},
}

// publishesFamilyVersion describes one package_registry.package_version
// fact in the Odù.
type publishesFamilyVersion struct {
	// PackageID is the owning package.
	PackageID string
	// VersionID is the version node's identity: the PackageVersion edge
	// target.
	VersionID string
	// Version is the raw version string.
	Version string
}

// publishesFamilyVersions holds the version facts: one owned version (the
// publication edge target) and one stale version (proving the tombstoned
// remote stays out on the publication path too).
var publishesFamilyVersions = []publishesFamilyVersion{
	{PackageID: publishesFamilyPackageID, VersionID: "ver-team-api-1.0.0", Version: "1.0.0"},
	{PackageID: "pkg:npm:@acme/stale-api", VersionID: "ver-stale-api-9.9.9", Version: "9.9.9"},
}

// publishesFamilyRepository describes one repository fact in the Odù.
type publishesFamilyRepository struct {
	// RepositoryID is the repository node id.
	RepositoryID string
	// Name is the repository display name.
	Name string
	// RemoteURL is the remote hints match against.
	RemoteURL string
	// Tombstone retires the fact: hints matching only it go stale.
	Tombstone bool
}

// publishesFamilyRepositories holds the repository facts: the owner, the
// tombstoned stale remote, and the two active mirrors sharing one remote
// for the ambiguity branch.
var publishesFamilyRepositories = []publishesFamilyRepository{
	{RepositoryID: publishesFamilyRepositoryID, Name: "team-api", RemoteURL: publishesFamilyRemoteURL},
	{RepositoryID: "repository:eshu-fixture-stale-api", Name: "stale-api", RemoteURL: "https://github.com/acme/stale-api", Tombstone: true},
	{RepositoryID: "repository:eshu-fixture-shared-a", Name: "shared-lib-a", RemoteURL: "https://github.com/acme/shared-lib"},
	{RepositoryID: "repository:eshu-fixture-shared-b", Name: "shared-lib-b", RemoteURL: "https://github.com/acme/shared-lib"},
}

// PublishesFamilyOdu builds the cataloged Odù for the publishes
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. Source hints, the
// package fact and versions are built as typed packageregistryv1 values
// and encoded through factschema.EncodePackageRegistrySourceHint /
// EncodePackageRegistryPackage / EncodePackageRegistryPackageVersion,
// never as hand-built maps (Contract System v1). The repository facts
// mirror the collector's payload shape.
// It panics on an encode failure for the same reason
// WorkloadCloudRelationshipFamilyOdu does: a failure means the payload
// contract moved under a committed fixture, and every coverage claim built
// on it is already void.
func PublishesFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0,
		len(publishesFamilyHints)+len(publishesFamilyVersions)+len(publishesFamilyRepositories)+1)
	for _, repository := range publishesFamilyRepositories {
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          publishesFamilyScopeID,
			GenerationID:     publishesFamilyGenerationID,
			FactKind:         factload.FactKindRepository,
			StableFactKey:    "repository:" + repository.RepositoryID,
			SchemaVersion:    publishesFamilySchemaVersion,
			CollectorKind:    publishesFamilyGitCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			IsTombstone:      repository.Tombstone,
			Payload: map[string]any{
				"graph_id":      repository.RepositoryID,
				"graph_kind":    "repository",
				"repo_id":       repository.RepositoryID,
				"name":          repository.Name,
				"remote_url":    repository.RemoteURL,
				"source_run_id": publishesFamilySourceRunID,
			},
		})
	}
	// The owned package's package_registry.package fact commits the
	// Package node the ownership edge resolves against: the writer's
	// MATCH binds (target:Package {uid: row.package_id}), and no hint
	// or version fact produces that node. It is decision-neutral by
	// construction — both correlation builders filter on their own
	// fact kinds (source_hint plus repository or package_version), so
	// the offline expected set is unchanged by its presence.
	packagePayload, err := factschema.EncodePackageRegistryPackage(packageregistryv1.Package{
		PackageID: publishesFamilyPackageID,
		Ecosystem: stringPtr("npm"),
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode package payload for %q: %v",
			PublishesFamilyOduName, publishesFamilyPackageID, err,
		))
	}
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          publishesFamilyScopeID,
		GenerationID:     publishesFamilyGenerationID,
		FactKind:         chain.PackageRegistryPackageFactKind,
		StableFactKey:    "package:" + publishesFamilyPackageID,
		SchemaVersion:    publishesFamilySchemaVersion,
		CollectorKind:    publishesFamilyRegistryCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload:          packagePayload,
	})
	for _, hint := range publishesFamilyHints {
		versionKey := "-"
		typed := packageregistryv1.SourceHint{
			PackageID:        hint.PackageID,
			HintKind:         hint.HintKind,
			RawURL:           stringPtr(hint.URL),
			NormalizedURL:    stringPtr(hint.URL),
			ConfidenceReason: stringPtr("ifa publishes family fixture"),
		}
		if hint.VersionID != "" {
			versionKey = hint.VersionID
			typed.VersionID = stringPtr(hint.VersionID)
		}
		payload, err := factschema.EncodePackageRegistrySourceHint(typed)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode source_hint payload for %q: %v",
				PublishesFamilyOduName, hint.PackageID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          publishesFamilyScopeID,
			GenerationID:     publishesFamilyGenerationID,
			FactKind:         chain.PackageRegistrySourceHintFactKind,
			StableFactKey:    fmt.Sprintf("package-hint:%s:%s:%s:%s", hint.PackageID, versionKey, hint.HintKind, hint.URL),
			SchemaVersion:    publishesFamilySchemaVersion,
			CollectorKind:    publishesFamilyRegistryCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}
	for _, version := range publishesFamilyVersions {
		payload, err := factschema.EncodePackageRegistryPackageVersion(packageregistryv1.PackageVersion{
			PackageID: version.PackageID,
			VersionID: version.VersionID,
			Version:   version.Version,
			Ecosystem: stringPtr("npm"),
		})
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode package_version payload for %q: %v",
				PublishesFamilyOduName, version.VersionID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          publishesFamilyScopeID,
			GenerationID:     publishesFamilyGenerationID,
			FactKind:         chain.PackageRegistryPackageVersionFactKind,
			StableFactKey:    "package-version:" + version.VersionID,
			SchemaVersion:    publishesFamilySchemaVersion,
			CollectorKind:    publishesFamilyRegistryCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: PublishesFamilyOduName, Facts: factsForOdu},
		Detail: "thirteen facts for the direct-materialization publishes family: four repositories, one package, six source hints and two versions — three edge-producing resolutions (ownership Package, ownership PackageVersion, publication PackageVersion) plus the stale, rejected, unresolved and ambiguous non-producers on both paths, so the PUBLISHES expected set proves both write templates and both evidence sources with restraint",
	}
}
