// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

// The kubernetes_correlation family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.KubernetesCorrelationEdgeWriter through the
// WriteKubernetesCorrelationEdges port. The relationship type is RUNS_IMAGE,
// a static token in the template rather than a per-row substitution; the
// source-node label is the validated static token from the closed source
// vocabulary (OciImageManifest / OciImageIndex / OciImageDescriptor)
// substituted into the canonical upsert template's one %s. The MERGE keys
// on the stable (workload_uid, RUNS_IMAGE, source_uid) triple. Read both
// off the template and the writer consts, never by deriving from the port
// or family name — "KUBERNETES_CORRELATION" is statement metadata, not a
// graph relationship type, and a name-derived literal would match no
// executed statement.
//
// Facts are built as typed kuberneteslivev1.PodTemplate /
// ociregistryv1.ImageManifest / ociregistryv1.ImageIndex /
// ociregistryv1.TagObservation values and encoded through
// factschema.EncodeKubernetesLivePodTemplate / EncodeOCIImageManifest /
// EncodeOCIImageIndex / EncodeOCIImageTagObservation, never as hand-built
// maps (Contract System v1).
//
// Scope note: every fact below shares one synthetic scope. Live, pod
// templates arrive in a cluster scope and OCI observations in per-repository
// registry scopes; the cassette replay ingests the whole Odù as one scope
// generation, the same single-generation shape the s3_logs_to Odù uses, so
// the extractor sees the same fact set the handler joins.
//
// That one scope is stamped source_system='oci_registry' in the committed
// cassette (scope descriptors are replay-transport, not Odù truth, so the
// lockstep projection ignores them): replay stamps fact.source_system
// from the scope, and the container-image-identity loader the handler
// reads only accepts OCI facts with source_system='oci_registry'. A
// kubernetes_live stamp would land the OCI facts invisible to that loader
// and the live cell would materialize zero edges (diagnosed live
// 2026-09-28). Carrying the OCI facts in a second seed scope instead was
// tried and reverted: the same logical facts in two scopes race
// last-writer-wins on the node provenance columns and N=1 vs N=4 dumps
// diverge, so duplicate substrate across scopes is out and the one scope
// carries the stamp the join needs.

const (
	// KubernetesCorrelationFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:kubernetes_correlation coverage row would
	// name to resolve through it.
	KubernetesCorrelationFamilyOduName = "odu:ifa-kubernetes-correlation-family"

	// kubernetesCorrelationFamilyScopeID is the single synthetic scope
	// every fact in this Odù belongs to. The reducer handler loads one
	// scope generation's facts, so a fixture spanning scopes would not
	// mirror any real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges — diagnosed live 2026-09-27 on the shared scope during the
	// iam_can_assume drive. One live generation per scope per cell is the
	// contract.
	kubernetesCorrelationFamilyScopeID = "k8s:eshu-fixture-kubernetes-correlation"

	// kubernetesCorrelationFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	kubernetesCorrelationFamilyGenerationID = "gen-ifa-kubernetes-correlation-family-1"

	// kubernetesCorrelationFamilyClusterID is the operator-declared
	// cluster identity every pod template below carries.
	kubernetesCorrelationFamilyClusterID = "prod-us-east-1"

	// kubernetesCorrelationFamilyNamespace is the namespace every fixture
	// workload below lives in.
	kubernetesCorrelationFamilyNamespace = "checkout"

	// kubernetesCorrelationFamilyRegistry is the registry every image
	// reference below names.
	kubernetesCorrelationFamilyRegistry = "registry.example.com"

	// kubernetesCorrelationFamilyK8sCollectorKind mirrors what the
	// kubernetes_live collector stamps on pod template facts, so the Odù
	// describes the same envelopes a live generation would carry rather
	// than agreeing only on the payload.
	kubernetesCorrelationFamilyK8sCollectorKind = "kubernetes_live"

	// kubernetesCorrelationFamilyOCICollectorKind mirrors what the
	// oci_registry collector stamps on manifest, index, and tag
	// observation facts.
	kubernetesCorrelationFamilyOCICollectorKind = "oci_registry"

	// kubernetesCorrelationFamilySourceConfidence marks these facts as
	// collector-reported, what both the kubernetes_live and oci_registry
	// collectors stamp (facts.SourceConfidenceReported).
	kubernetesCorrelationFamilySourceConfidence = "reported"
)

// The fixture digests. checkout-digest and billing-digest are carried by
// active deployment sources (the edges); legacy-digest is carried only by a
// tombstoned manifest (stale, never exact); phantom-digest names a digest
// no source observes (unresolved); canary-digest-a/b are the two digests
// one mutable tag resolves to (ambiguous).
const (
	kubernetesCorrelationFamilyCheckoutDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	kubernetesCorrelationFamilyBillingDigest  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	kubernetesCorrelationFamilyLegacyDigest   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	kubernetesCorrelationFamilyPhantomDigest  = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	kubernetesCorrelationFamilyCanaryDigestA  = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	kubernetesCorrelationFamilyCanaryDigestB  = "sha256:6666666666666666666666666666666666666666666666666666666666666666"
)

// The fixture repositories, registry-qualified as the live image
// references spell them.
const (
	kubernetesCorrelationFamilyCheckoutRepository = "team/checkout"
	kubernetesCorrelationFamilyBillingRepository  = "team/billing"
	kubernetesCorrelationFamilyLegacyRepository   = "team/legacy"
	kubernetesCorrelationFamilyCanaryRepository   = "team/canary"
	kubernetesCorrelationFamilyPhantomRepository  = "team/phantom"
)

// kubernetesCorrelationFamilyRepositoryID returns the oci-registry://
// repository identity the descriptor uid derives from.
func kubernetesCorrelationFamilyRepositoryID(repository string) string {
	return "oci-registry://" + kubernetesCorrelationFamilyRegistry + "/" + repository
}

// kubernetesCorrelationFamilyDescriptorID returns the collector-resolved
// descriptor uid for one fixture source: the value the canonical projector
// writes as the OCI node uid, which the digest join index prefers when the
// payload carries it.
func kubernetesCorrelationFamilyDescriptorID(repository, digest string) string {
	return "oci-descriptor://" + kubernetesCorrelationFamilyRegistry + "/" + repository + "@" + digest
}

// kubernetesCorrelationFamilyImageRef returns the digest-form live image
// reference one fixture workload declares.
func kubernetesCorrelationFamilyImageRef(repository, digest string) string {
	return kubernetesCorrelationFamilyRegistry + "/" + repository + "@" + digest
}

// kubernetesCorrelationFamilyWorkloadObjectID returns the collector-derived
// stable identity for one fixture workload: it anchors the
// KubernetesWorkload node the RUNS_IMAGE edge leaves from.
func kubernetesCorrelationFamilyWorkloadObjectID(name string) string {
	return "k8s://" + kubernetesCorrelationFamilyClusterID + "/apps/v1/deployments/" + kubernetesCorrelationFamilyNamespace + "/" + name
}

// The fixture workload names. checkout and billing resolve exact digests
// against active sources (the edges); legacy names a tombstone-only digest
// (stale); canary names a tag two digests share (ambiguous); phantom names
// a digest no source observes (unresolved).
const (
	kubernetesCorrelationFamilyCheckoutWorkload = "checkout"
	kubernetesCorrelationFamilyBillingWorkload  = "billing"
	kubernetesCorrelationFamilyLegacyWorkload   = "legacy"
	kubernetesCorrelationFamilyCanaryWorkload   = "canary"
	kubernetesCorrelationFamilyPhantomWorkload  = "phantom"
)

// kubernetesCorrelationFamilySourceFixture describes one OCI
// digest-addressed source fact in the Odù: a manifest or an index whose
// digest the classifier resolves live digests against and whose node uid
// the digest join index anchors the edge target on.
type kubernetesCorrelationFamilySourceFixture struct {
	// Repository is the registry-qualified repository name, e.g.
	// team/checkout.
	Repository string
	// Digest is the source content digest.
	Digest string
	// Index selects the fact kind: false emits an image_manifest fact
	// (OciImageManifest node), true an image_index fact (OciImageIndex
	// node), proving the two digest-addressed source labels the template
	// MATCHes.
	Index bool
	// Tombstone marks the source deleted: the legacy manifest arrives
	// tombstoned, so the live digest that names it classifies stale and
	// the digest join index drops it — no edge to a node that no longer
	// exists.
	Tombstone bool
}

// kubernetesCorrelationFamilySources are the digest-addressed source
// substrate: an active manifest, an active index, and a tombstoned
// manifest. The descriptor-kind label (OciImageDescriptor) is not
// exercised: no collector emits descriptor-kind facts in this fixture's
// corpus, and inventing one would prove a path no live generation carries.
var kubernetesCorrelationFamilySources = []kubernetesCorrelationFamilySourceFixture{
	{
		Repository: kubernetesCorrelationFamilyCheckoutRepository,
		Digest:     kubernetesCorrelationFamilyCheckoutDigest,
	},
	{
		Repository: kubernetesCorrelationFamilyBillingRepository,
		Digest:     kubernetesCorrelationFamilyBillingDigest,
		Index:      true,
	},
	{
		Repository: kubernetesCorrelationFamilyLegacyRepository,
		Digest:     kubernetesCorrelationFamilyLegacyDigest,
		Tombstone:  true,
	},
}

// kubernetesCorrelationFamilyTagFixture describes one tag observation: a
// mutable tag-to-digest sighting. Two observations share the canary tag
// with different digests, the ambiguous restraint.
type kubernetesCorrelationFamilyTagFixture struct {
	// Repository is the registry-qualified repository name.
	Repository string
	// Tag is the mutable tag name observed.
	Tag string
	// ResolvedDigest is the digest the tag resolved to at observation
	// time.
	ResolvedDigest string
}

// kubernetesCorrelationFamilyTags are the tag observations: one mutable
// tag resolving to two digests, so the workload naming it classifies
// ambiguous rather than promoting whichever observation arrived first.
var kubernetesCorrelationFamilyTags = []kubernetesCorrelationFamilyTagFixture{
	{
		Repository:     kubernetesCorrelationFamilyCanaryRepository,
		Tag:            "stable",
		ResolvedDigest: kubernetesCorrelationFamilyCanaryDigestA,
	},
	{
		Repository:     kubernetesCorrelationFamilyCanaryRepository,
		Tag:            "stable",
		ResolvedDigest: kubernetesCorrelationFamilyCanaryDigestB,
	},
}

// kubernetesCorrelationFamilyWorkloadFixture describes one pod template
// fact in the Odù: a live workload declaring one image reference.
type kubernetesCorrelationFamilyWorkloadFixture struct {
	// Name is the deployment name: the workload object_id tail and the
	// stable key identity.
	Name string
	// ImageRef is the declared live image reference the classifier joins
	// to the source index.
	ImageRef string
}

// kubernetesCorrelationFamilyWorkloads are the live workloads: two edge
// producers (digest-form refs matching active sources), one naming a
// tombstone-only digest (stale, the conservative skip), one naming a
// tag two digests share (ambiguous, never promoted to exact), and one
// naming a digest no source observes (unresolved).
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — a stale, ambiguous, or unresolved decision is
// not edge-eligible — and the writer's two MATCH clauses would no-op on a
// missing node anyway. Without them, a regression that promoted every
// decision to an edge regardless of outcome would still reproduce the
// expected set exactly and this fixture would report green.
var kubernetesCorrelationFamilyWorkloads = []kubernetesCorrelationFamilyWorkloadFixture{
	{
		// EDGE: digest-form ref matches the active checkout manifest
		// digest (exact, OciImageManifest target).
		Name:     kubernetesCorrelationFamilyCheckoutWorkload,
		ImageRef: kubernetesCorrelationFamilyImageRef(kubernetesCorrelationFamilyCheckoutRepository, kubernetesCorrelationFamilyCheckoutDigest),
	},
	{
		// EDGE: digest-form ref matches the active billing index digest
		// (exact, OciImageIndex target).
		Name:     kubernetesCorrelationFamilyBillingWorkload,
		ImageRef: kubernetesCorrelationFamilyImageRef(kubernetesCorrelationFamilyBillingRepository, kubernetesCorrelationFamilyBillingDigest),
	},
	{
		// NO EDGE: the only source carrying this digest is tombstoned —
		// stale, the conservative skip.
		Name:     kubernetesCorrelationFamilyLegacyWorkload,
		ImageRef: kubernetesCorrelationFamilyImageRef(kubernetesCorrelationFamilyLegacyRepository, kubernetesCorrelationFamilyLegacyDigest),
	},
	{
		// NO EDGE: the stable tag resolves to two digests — ambiguous,
		// never promoted to exact.
		Name:     kubernetesCorrelationFamilyCanaryWorkload,
		ImageRef: kubernetesCorrelationFamilyRegistry + "/" + kubernetesCorrelationFamilyCanaryRepository + ":stable",
	},
	{
		// NO EDGE: no source observes this digest — unresolved.
		Name:     kubernetesCorrelationFamilyPhantomWorkload,
		ImageRef: kubernetesCorrelationFamilyImageRef(kubernetesCorrelationFamilyPhantomRepository, kubernetesCorrelationFamilyPhantomDigest),
	},
}

// kubernetesCorrelationFamilyStableTagKey derives one tag observation's
// durable dedup key. The collector keys tag observations by hex digest over
// (digest, repository_id, tag); the fixture-local form carries the same
// three identity inputs in readable form instead of hand-typing a string
// the expected-edge fixture cannot independently check. Both canary
// observations share repository and tag and differ only in digest, so the
// digest MUST be in the key: without it the two observations collapse
// last-writer-wins at live ingest, the tag resolves to one digest, and the
// ambiguous restraint never reaches the extractor.
func kubernetesCorrelationFamilyStableTagKey(fixture kubernetesCorrelationFamilyTagFixture) string {
	return fmt.Sprintf(
		"oci-registry:tag-observation:%s:%s:%s",
		kubernetesCorrelationFamilyRepositoryID(fixture.Repository),
		fixture.Tag,
		fixture.ResolvedDigest,
	)
}

// kubernetesCorrelationFamilyPodTemplatePayload builds one typed pod
// template payload through the contract encoder. ObjectID, cluster,
// namespace, name, uid, image refs, one container carrying the ref, and the
// correlation anchors mirror what the kubernetes_live collector emits; the
// stable key IS the object_id on both sides.
func kubernetesCorrelationFamilyPodTemplatePayload(fixture kubernetesCorrelationFamilyWorkloadFixture) map[string]any {
	objectID := kubernetesCorrelationFamilyWorkloadObjectID(fixture.Name)
	clusterID := kubernetesCorrelationFamilyClusterID
	namespace := kubernetesCorrelationFamilyNamespace
	name := fixture.Name
	uid := "uid-" + fixture.Name
	groupVersionResource := "apps/v1/deployments"
	serviceAccount := "default"
	containerName := fixture.Name + "-c0"
	init := false
	imageRef := fixture.ImageRef
	selector := map[string]string{"app": fixture.Name}
	payload, err := factschema.EncodeKubernetesLivePodTemplate(kuberneteslivev1.PodTemplate{
		ObjectID:             objectID,
		ClusterID:            &clusterID,
		Namespace:            &namespace,
		Name:                 &name,
		WorkloadUID:          &uid,
		GroupVersionResource: &groupVersionResource,
		ServiceAccount:       &serviceAccount,
		Containers: []kuberneteslivev1.PodTemplateContainer{
			{
				Name:  &containerName,
				Image: &imageRef,
				Init:  &init,
			},
		},
		ImageRefs:          []string{fixture.ImageRef},
		Selector:           selector,
		Labels:             selector,
		CorrelationAnchors: []string{objectID, fixture.ImageRef},
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode pod_template payload for %q: %v",
			KubernetesCorrelationFamilyOduName, fixture.Name, err,
		))
	}
	return payload
}

// KubernetesCorrelationFamilyOdu builds the cataloged Odù for the
// kubernetes_correlation direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason EC2UsesProfileFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func KubernetesCorrelationFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0,
		len(kubernetesCorrelationFamilySources)+len(kubernetesCorrelationFamilyTags)+len(kubernetesCorrelationFamilyWorkloads))
	for _, fixture := range kubernetesCorrelationFamilySources {
		repositoryID := kubernetesCorrelationFamilyRepositoryID(fixture.Repository)
		descriptorID := kubernetesCorrelationFamilyDescriptorID(fixture.Repository, fixture.Digest)
		factKind := facts.OCIImageManifestFactKind
		schemaVersion := facts.OCIImageManifestSchemaVersion
		var payload map[string]any
		var err error
		if fixture.Index {
			factKind = facts.OCIImageIndexFactKind
			schemaVersion = facts.OCIImageIndexSchemaVersion
			payload, err = factschema.EncodeOCIImageIndex(ociregistryv1.ImageIndex{
				RepositoryID: repositoryID,
				Digest:       fixture.Digest,
				DescriptorID: &descriptorID,
			})
		} else {
			payload, err = factschema.EncodeOCIImageManifest(ociregistryv1.ImageManifest{
				RepositoryID: repositoryID,
				Digest:       fixture.Digest,
				DescriptorID: &descriptorID,
			})
		}
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode OCI source payload for %q: %v",
				KubernetesCorrelationFamilyOduName, repositoryID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          kubernetesCorrelationFamilyScopeID,
			GenerationID:     kubernetesCorrelationFamilyGenerationID,
			FactKind:         factKind,
			StableFactKey:    descriptorID,
			SchemaVersion:    schemaVersion,
			CollectorKind:    kubernetesCorrelationFamilyOCICollectorKind,
			SourceConfidence: kubernetesCorrelationFamilySourceConfidence,
			IsTombstone:      fixture.Tombstone,
			Payload:          payload,
		})
	}
	for _, fixture := range kubernetesCorrelationFamilyTags {
		payload, err := factschema.EncodeOCIImageTagObservation(ociregistryv1.TagObservation{
			RepositoryID:   kubernetesCorrelationFamilyRepositoryID(fixture.Repository),
			Tag:            fixture.Tag,
			ResolvedDigest: fixture.ResolvedDigest,
		})
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode tag observation payload for %q: %v",
				KubernetesCorrelationFamilyOduName, kubernetesCorrelationFamilyStableTagKey(fixture), err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          kubernetesCorrelationFamilyScopeID,
			GenerationID:     kubernetesCorrelationFamilyGenerationID,
			FactKind:         facts.OCIImageTagObservationFactKind,
			StableFactKey:    kubernetesCorrelationFamilyStableTagKey(fixture),
			SchemaVersion:    facts.OCIImageTagObservationSchemaVersion,
			CollectorKind:    kubernetesCorrelationFamilyOCICollectorKind,
			SourceConfidence: kubernetesCorrelationFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range kubernetesCorrelationFamilyWorkloads {
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          kubernetesCorrelationFamilyScopeID,
			GenerationID:     kubernetesCorrelationFamilyGenerationID,
			FactKind:         facts.KubernetesPodTemplateFactKind,
			StableFactKey:    kubernetesCorrelationFamilyWorkloadObjectID(fixture.Name),
			SchemaVersion:    facts.KubernetesPodTemplateSchemaVersion,
			CollectorKind:    kubernetesCorrelationFamilyK8sCollectorKind,
			SourceConfidence: kubernetesCorrelationFamilySourceConfidence,
			Payload:          kubernetesCorrelationFamilyPodTemplatePayload(fixture),
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: KubernetesCorrelationFamilyOduName, Facts: factsForOdu},
		Detail: "ten facts for the direct-materialization kubernetes_correlation family: two active OCI digest-addressed sources (one manifest, one index) plus one tombstoned manifest, two tag observations sharing one tag with different digests, and five pod templates (two digest-form refs matching the active sources, one naming the tombstone-only digest, one naming the ambiguous tag, and one naming an unobserved digest), so the RUNS_IMAGE expected set proves the exact-digest resolution mode and restraint",
	}
}
