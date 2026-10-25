// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	ociregistryv1 "github.com/eshu-hq/eshu-sdk/factschema/ociregistry/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

const (
	// BuiltFromFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:built_from coverage row would name to resolve
	// through it.
	BuiltFromFamilyOduName = "odu:ifa-built-from-family"

	// builtFromFamilyScopeID is the single scope every fact in this Odù
	// belongs to: the owning repository's scope, which is also the scope
	// the writing intent carries and the writer stamps as scope_id. Live,
	// the handler assembles this same flat set from its own-scope load
	// plus the cross-scope active loads (identity, SLSA, CI,
	// repositories); the extractor itself never filters by scope, so the
	// single-scope flattening is behavior-preserving for the offline
	// proof. The live cell must drive the writing intent in this scope,
	// or update the fixture's identity pins to the scope it uses — the
	// guard test reds on a mismatch either way.
	builtFromFamilyScopeID = "git-repository-scope:repository:eshu-fixture-orders-api"

	// builtFromFamilyGenerationID is the single generation both scopes
	// replay.
	builtFromFamilyGenerationID = "gen-ifa-built-from-family-1"

	// builtFromFamilyRepositoryID is the repository node id the built
	// images resolve to. It is the Repository node id the write template
	// MATCHes (repo:Repository {id: row.repository_id}), so it is what the
	// expected-edge fixture's target identities name.
	builtFromFamilyRepositoryID = "repository:eshu-fixture-orders-api"

	// builtFromFamilyRemoteURL is the repository's remote. The OCI config
	// source label names this URL, and the pipeline matches it against the
	// repository fact's remote_url by canonical key.
	builtFromFamilyRemoteURL = "https://github.com/acme/orders-api"

	// builtFromFamilySourceRunID is the collector run stamped onto the
	// repository fact. The projector's canonical phase drops a decoded
	// repository fact with an empty source run id (no code_entities_uid
	// phase row), so the committed cassette must carry the same value or
	// the guard test reds on the drift either way.
	builtFromFamilySourceRunID = "run-ifa-built-from-family-1"

	// builtFromFamilySchemaVersion, builtFromFamilyOCICollectorKind and
	// builtFromFamilyManifestMediaType mirror what the collectors stamp
	// on repository, content_entity and OCI manifest facts.
	builtFromFamilySchemaVersion     = "1.0.0"
	builtFromFamilyOCICollectorKind  = "oci_registry"
	builtFromFamilyGitCollectorKind  = "git"
	builtFromFamilyManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
)

// builtFromFamilyDigestApp and builtFromFamilyDigestWorker are the two
// exact digests whose OCI configs declare the fixture repository as their
// builder: the two edge producers. The third-party and ghost digests below
// are deliberate non-producers.
const (
	builtFromFamilyDigestApp        = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	builtFromFamilyDigestThirdParty = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	builtFromFamilyDigestGhost      = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	builtFromFamilyDigestWorker     = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	builtFromFamilyImageRepo        = "oci-registry://registry.example.com/acme/orders-api"
	builtFromFamilyThirdPartyRepo   = "oci-registry://registry.example.com/thirdparty/tool"
	builtFromFamilyGhostRepo        = "oci-registry://registry.example.com/acme/ghost-svc"
	builtFromFamilyGhostRemoteURL   = "https://github.com/acme/ghost-svc"
)

// builtFromFamilyManifest describes one OCI manifest fact in the Odù.
type builtFromFamilyManifest struct {
	// Digest is the manifest's content digest: the edge source identity.
	Digest string
	// RepositoryID is the owning registry repository.
	RepositoryID string
	// SourceLabel is the OCI config source-label value naming the builder
	// repository. Empty means no build evidence (a plain registry
	// observation); a URL with no repository fact means unscanned.
	SourceLabel string
}

// builtFromFamilyManifests holds the registry side of the fixture: two
// edge-producing manifests built by the fixture repository and two
// deliberate non-producers covering the no-build-evidence and
// unscanned-builder branches.
var builtFromFamilyManifests = []builtFromFamilyManifest{
	// EDGE: exact digest whose OCI config names the fixture repository.
	{Digest: builtFromFamilyDigestApp, RepositoryID: builtFromFamilyImageRepo, SourceLabel: builtFromFamilyRemoteURL},
	// EDGE: a second digest from the same builder. The row builder dedups
	// by (digest, repository), so a shared builder fans out to one edge
	// per digest rather than collapsing to one.
	{Digest: builtFromFamilyDigestWorker, RepositoryID: builtFromFamilyImageRepo, SourceLabel: builtFromFamilyRemoteURL},
	// NO EDGE: an exact digest with no build evidence. The registry
	// observation classifies it exact_digest, but nothing attributes it to
	// the fixture repository.
	{Digest: builtFromFamilyDigestThirdParty, RepositoryID: builtFromFamilyThirdPartyRepo},
	// NO EDGE: build evidence naming a repository no fact scans. The label
	// matches zero repositories, so no provenance is conferred.
	{Digest: builtFromFamilyDigestGhost, RepositoryID: builtFromFamilyGhostRepo, SourceLabel: builtFromFamilyGhostRemoteURL},
}

// BuiltFromFamilyOdu builds the cataloged Odù for the built_from
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. OCI manifests are
// built as typed ociregistryv1.ImageManifest values and encoded through
// factschema.EncodeOCIImageManifest, never as hand-built maps (Contract
// System v1). The repository and Kubernetes-reference facts mirror the
// collector's payload shape. It panics on an encode failure for the same
// reason WorkloadCloudRelationshipFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func BuiltFromFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(builtFromFamilyManifests)+2)
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          builtFromFamilyScopeID,
		GenerationID:     builtFromFamilyGenerationID,
		FactKind:         factload.FactKindRepository,
		StableFactKey:    "repository:" + builtFromFamilyRepositoryID,
		SchemaVersion:    builtFromFamilySchemaVersion,
		CollectorKind:    builtFromFamilyGitCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"graph_id":      builtFromFamilyRepositoryID,
			"graph_kind":    "repository",
			"repo_id":       builtFromFamilyRepositoryID,
			"name":          "orders-api",
			"remote_url":    builtFromFamilyRemoteURL,
			"source_run_id": builtFromFamilySourceRunID,
		},
	})
	// A Kubernetes manifest in the fixture repository merely REFERENCES
	// the digest-pinned third-party image. That anchors
	// SourceRepositoryIDs on the deploying repository's own scope but is
	// not build evidence (#5796): claiming the repository built that image
	// would fabricate CVE-inheritance lineage from its Dockerfile base.
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          builtFromFamilyScopeID,
		GenerationID:     builtFromFamilyGenerationID,
		FactKind:         ContentEntityFactKind,
		StableFactKey:    "content_entity:k8s-ref-thirdparty-tool",
		SchemaVersion:    builtFromFamilySchemaVersion,
		CollectorKind:    builtFromFamilyGitCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"repo_id":     builtFromFamilyRepositoryID,
			"entity_type": "K8sResource",
			"entity_metadata": map[string]any{
				"container_images": []any{"registry.example.com/thirdparty/tool@" + builtFromFamilyDigestThirdParty},
			},
		},
	})
	for _, manifest := range builtFromFamilyManifests {
		typed := ociregistryv1.ImageManifest{
			RepositoryID: manifest.RepositoryID,
			Digest:       manifest.Digest,
			MediaType:    stringPtr(builtFromFamilyManifestMediaType),
		}
		if manifest.SourceLabel != "" {
			typed.ConfigLabels = map[string]string{
				"org.opencontainers.image.source": manifest.SourceLabel,
			}
		}
		payload, err := factschema.EncodeOCIImageManifest(typed)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode oci manifest payload for %q: %v",
				BuiltFromFamilyOduName, manifest.Digest, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          builtFromFamilyScopeID,
			GenerationID:     builtFromFamilyGenerationID,
			FactKind:         chain.OCIImageManifestFactKind,
			StableFactKey:    "oci-manifest:" + manifest.Digest,
			SchemaVersion:    builtFromFamilySchemaVersion,
			CollectorKind:    builtFromFamilyOCICollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: BuiltFromFamilyOduName, Facts: factsForOdu},
		Detail: "six facts for the direct-materialization built_from family: one repository, four OCI manifests (two edge-producing builds plus the no-build-evidence and unscanned-builder non-producers) and one deploy-only Kubernetes reference proving a runtime reference never confers build provenance, so the BUILT_FROM expected set proves both promotion and restraint",
	}
}
