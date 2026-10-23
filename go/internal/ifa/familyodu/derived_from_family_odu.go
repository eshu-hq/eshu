// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

const (
	// DerivedFromFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:derived_from coverage row would name to resolve
	// through it.
	DerivedFromFamilyOduName = "odu:ifa-derived-from-family"

	// derivedFromFamilyScopeID is the single scope every fact in this Odù
	// belongs to: the owning repository's scope, which is also the scope
	// the writing intent carries and the writer stamps as scope_id. Live,
	// the handler assembles this same flat set from its own-scope load
	// plus the cross-scope active loads; the extractor itself never
	// filters by scope, so the single-scope flattening is
	// behavior-preserving for the offline proof. The live cell must drive
	// the writing intent in this scope, or update the fixture's identity
	// pins to the scope it uses — the guard test reds on a mismatch
	// either way.
	derivedFromFamilyScopeID = "git-repository-scope:repository:eshu-fixture-lineage-app"

	// derivedFromFamilyGenerationID is the one scope generation the Odù
	// replays.
	derivedFromFamilyGenerationID = "gen-ifa-derived-from-family-1"

	// derivedFromFamilyRepositoryID is the owning repository: the
	// repository whose Dockerfile declares the base and whose builds are
	// the DERIVED_FROM children. It is what the guard passes as the
	// owning repository, derived from the Odù's single repository fact.
	derivedFromFamilyRepositoryID = "repository:eshu-fixture-lineage-app"

	// derivedFromFamilyRemoteURL is the owning repository's remote. The
	// children's OCI config source labels name this URL.
	derivedFromFamilyRemoteURL = "https://github.com/acme/lineage-app"

	// derivedFromFamilyLocalPath is the owning repository's checkout
	// path. The codegraph extractor falls back to local_path for the
	// Repository.path it MERGEs, and Repository.path carries a global
	// UNIQUE constraint: without this, the family's node takes path=""
	// and collides with every other path-less repository in the same
	// cell (the maintenance pass then retries the commit until the 30s
	// write deadline reports a timeout). Unique per repository by
	// fixture construction, mirroring crossplane's /repo-crossplane-platform.
	derivedFromFamilyLocalPath = "/repo-lineage-app"

	// derivedFromFamilySchemaVersion and derivedFromFamilyCollectorKind
	// mirror what the collectors stamp on repository, file,
	// content_entity and OCI manifest facts.
	derivedFromFamilySchemaVersion    = "1.0.0"
	derivedFromFamilyCollectorKind    = "git"
	derivedFromFamilyOCICollectorKind = "oci_registry"

	// derivedFromFamilyManifestMediaType is the OCI media type every
	// manifest fact carries.
	derivedFromFamilyManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
)

// derivedFromFamilyDigestBase is the exact digest the owning Dockerfile
// declares as its runtime base: the single distinct base every child edge
// points at.
const derivedFromFamilyDigestBase = "sha256:b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1"

// derivedFromFamilyDigestChild1 and derivedFromFamilyDigestChild2 are the
// exact digests the owning repository built from that base: the two edge
// producers.
const (
	derivedFromFamilyDigestChild1 = "sha256:c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1"
	derivedFromFamilyDigestChild2 = "sha256:c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2"
)

// derivedFromFamilyDigestThirdParty is an exact digest the repository
// merely deploys: a deliberate non-producer.
const derivedFromFamilyDigestThirdParty = "sha256:e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1"

// derivedFromFamilyBaseRepo, derivedFromFamilyChildRepo and
// derivedFromFamilyThirdPartyRepo are the OCI registry repositories the
// base, the children and the third-party image are observed in.
const (
	derivedFromFamilyBaseRepo       = "oci-registry://registry.example.com/acme/base"
	derivedFromFamilyChildRepo      = "oci-registry://registry.example.com/acme/lineage-app"
	derivedFromFamilyThirdPartyRepo = "oci-registry://registry.example.com/thirdparty/tool"
	derivedFromFamilyBaseImageRef   = "registry.example.com/acme/base"
	derivedFromFamilyThirdPartyRef  = "registry.example.com/thirdparty/tool"
)

// derivedFromFamilyManifest describes one OCI manifest fact in the Odù.
type derivedFromFamilyManifest struct {
	// Digest is the manifest's content digest.
	Digest string
	// RepositoryID is the owning registry repository.
	RepositoryID string
	// BuiltByOwner marks a manifest whose OCI config declares the owning
	// repository as its builder: build evidence for the DERIVED_FROM
	// child side.
	BuiltByOwner bool
}

// derivedFromFamilyManifests holds the registry side of the fixture: the
// base both children derive from, the two edge-producing children, and
// the deploy-only third-party image.
var derivedFromFamilyManifests = []derivedFromFamilyManifest{
	// The Dockerfile-declared base. Its own config names the owning
	// repository too, so its decision carries both the base anchor and
	// build provenance — proving the self-loop skip: an image is never
	// its own ancestor, or CVE lineage traversal would cycle.
	{Digest: derivedFromFamilyDigestBase, RepositoryID: derivedFromFamilyBaseRepo, BuiltByOwner: true},
	// EDGE: exact child built by the owning repository from the base.
	{Digest: derivedFromFamilyDigestChild1, RepositoryID: derivedFromFamilyChildRepo, BuiltByOwner: true},
	// EDGE: a second child from the same base. The row builder dedups by
	// (child, base), so a shared base fans out to one edge per child.
	{Digest: derivedFromFamilyDigestChild2, RepositoryID: derivedFromFamilyChildRepo, BuiltByOwner: true},
	// NO EDGE: an exact digest the repository merely deploys. The
	// Kubernetes reference below anchors it without build evidence, and
	// claiming derivation from the Dockerfile base would fabricate CVE
	// lineage for an unrelated third-party image.
	{Digest: derivedFromFamilyDigestThirdParty, RepositoryID: derivedFromFamilyThirdPartyRepo},
}

// DerivedFromFamilyOdu builds the cataloged Odù for the derived_from
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. OCI manifests are
// built as typed ociregistryv1.ImageManifest values and encoded through
// factschema.EncodeOCIImageManifest, never as hand-built maps (Contract
// System v1). The repository, Dockerfile and Kubernetes-reference facts
// mirror the collector's payload shape. It panics on an encode failure for
// the same reason WorkloadCloudRelationshipFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func DerivedFromFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(derivedFromFamilyManifests)+3)
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          derivedFromFamilyScopeID,
		GenerationID:     derivedFromFamilyGenerationID,
		FactKind:         factload.FactKindRepository,
		StableFactKey:    "repository:" + derivedFromFamilyRepositoryID,
		SchemaVersion:    derivedFromFamilySchemaVersion,
		CollectorKind:    derivedFromFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"graph_id":      derivedFromFamilyRepositoryID,
			"graph_kind":    "repository",
			"repo_id":       derivedFromFamilyRepositoryID,
			"name":          "lineage-app",
			"remote_url":    derivedFromFamilyRemoteURL,
			"local_path":    derivedFromFamilyLocalPath,
			"source_run_id": "run-ifa-derived-from-family-1",
		},
	})
	// The owning Dockerfile declares exactly one distinct exact-digest
	// runtime base. A second distinct base would make attribution
	// ambiguous and project nothing — that conservative branch is pinned
	// by containerimage's own unit tests, not here, because an ambiguous
	// Odù would produce zero rows and the expected-edge loader rejects an
	// empty set as vacuous.
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          derivedFromFamilyScopeID,
		GenerationID:     derivedFromFamilyGenerationID,
		FactKind:         factload.FactKindFile,
		StableFactKey:    "file:" + derivedFromFamilyRepositoryID + ":Dockerfile",
		SchemaVersion:    derivedFromFamilySchemaVersion,
		CollectorKind:    derivedFromFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"repo_id":       derivedFromFamilyRepositoryID,
			"language":      "dockerfile",
			"relative_path": "Dockerfile",
			"parsed_file_data": map[string]any{
				"dockerfile_stages": []any{
					map[string]any{
						"stage_index": 0,
						"base_image":  derivedFromFamilyBaseImageRef + "@" + derivedFromFamilyDigestBase,
						"base_tag":    "",
					},
				},
			},
		},
	})
	// A Kubernetes manifest in the owning repository merely REFERENCES
	// the digest-pinned third-party image: a deploy anchor, never
	// derivation evidence.
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          derivedFromFamilyScopeID,
		GenerationID:     derivedFromFamilyGenerationID,
		FactKind:         ContentEntityFactKind,
		StableFactKey:    "content_entity:k8s-ref-thirdparty-tool",
		SchemaVersion:    derivedFromFamilySchemaVersion,
		CollectorKind:    derivedFromFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"repo_id":     derivedFromFamilyRepositoryID,
			"entity_type": "K8sResource",
			"entity_metadata": map[string]any{
				"container_images": []any{derivedFromFamilyThirdPartyRef + "@" + derivedFromFamilyDigestThirdParty},
			},
		},
	})
	for _, manifest := range derivedFromFamilyManifests {
		typed := ociregistryv1.ImageManifest{
			RepositoryID: manifest.RepositoryID,
			Digest:       manifest.Digest,
			MediaType:    stringPtr(derivedFromFamilyManifestMediaType),
		}
		if manifest.BuiltByOwner {
			typed.ConfigLabels = map[string]string{
				"org.opencontainers.image.source": derivedFromFamilyRemoteURL,
			}
		}
		payload, err := factschema.EncodeOCIImageManifest(typed)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode oci manifest payload for %q: %v",
				DerivedFromFamilyOduName, manifest.Digest, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          derivedFromFamilyScopeID,
			GenerationID:     derivedFromFamilyGenerationID,
			FactKind:         chain.OCIImageManifestFactKind,
			StableFactKey:    "oci-manifest:" + manifest.Digest,
			SchemaVersion:    derivedFromFamilySchemaVersion,
			CollectorKind:    derivedFromFamilyOCICollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: DerivedFromFamilyOduName, Facts: factsForOdu},
		Detail: "seven facts for the direct-materialization derived_from family: one repository, one single-base Dockerfile, four OCI manifests (the base, two edge-producing children, and the deploy-only third-party image) and one deploy-only Kubernetes reference, so the DERIVED_FROM expected set proves the conservative single-base attribution with its self-loop and deploy-only restraint",
	}
}
