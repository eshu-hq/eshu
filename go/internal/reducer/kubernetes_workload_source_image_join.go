package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SourceImageDigestJoinIndex resolves a live workload image's SourceDigest to
// the uid of the canonical ContainerImage / OCI manifest node the registry
// projector committed. It is the source-endpoint resolver the #388 live-workload
// edge slice (PR3) needs: the live (workload) side carries a raw image digest,
// while the canonical OCI manifest/index/descriptor node is keyed by a
// descriptor uid, so the edge cannot anchor its source endpoint without this
// digest -> uid bridge.
//
// The index is built once per edge slice from the active OCI digest-addressed
// facts (manifest, image index, reusable descriptor), so resolution is O(1) per
// edge — no per-edge graph round trip and no N+1 Cypher, mirroring the AWS
// relationship CloudResource join index (#805 §5.1). It never fabricates a uid:
// a digest resolves only when an active OCI fact carried that digest in this
// generation, and the uid it returns is exactly the one the canonical projector
// wrote (the fact's descriptor_id, or the deterministic descriptor uid derived
// from repository_id + digest when descriptor_id is absent, matching the
// projector's ociDescriptorUID fallback).
type SourceImageDigestJoinIndex struct {
	// byDigest maps a normalized digest -> canonical node uid. A digest is global
	// across registries in practice (it is the content address), so the digest is
	// the join key; the uid carries the repository scoping the projector wrote.
	byDigest map[string]string
}

// BuildSourceImageDigestJoinIndex builds the bounded in-memory digest -> node
// uid index from the supplied OCI digest-addressed fact envelopes. It is a pure
// function over fact envelopes (no I/O).
func BuildSourceImageDigestJoinIndex(envelopes []facts.Envelope) SourceImageDigestJoinIndex {
	index := SourceImageDigestJoinIndex{byDigest: make(map[string]string, len(envelopes))}
	// A tombstoned digest is a removed source; track which digests are seen active
	// so a later (or earlier) active observation overrides a tombstone regardless
	// of envelope order. Without this, an edge could resolve to a node that no
	// longer exists, or fail to resolve a digest that is genuinely still present.
	tombstoned := make(map[string]struct{})
	active := make(map[string]struct{})

	for _, env := range envelopes {
		if !isOCIDigestSourceFactKind(env.FactKind) {
			continue
		}
		digest := normalizeSourceDigest(payloadString(env.Payload, "digest"))
		if digest == "" {
			continue
		}
		uid := ociManifestNodeUID(env.Payload)
		if uid == "" {
			continue
		}
		index.byDigest[digest] = uid
		if env.IsTombstone {
			if _, seenActive := active[digest]; !seenActive {
				tombstoned[digest] = struct{}{}
			}
			continue
		}
		active[digest] = struct{}{}
		delete(tombstoned, digest)
	}

	for digest := range tombstoned {
		delete(index.byDigest, digest)
	}
	return index
}

// ResolveDigest returns the canonical node uid for a source digest, reporting
// whether an active digest-addressed source node carried that digest in this
// generation. An unknown or tombstone-only digest returns ("", false).
func (i SourceImageDigestJoinIndex) ResolveDigest(digest string) (string, bool) {
	uid, ok := i.byDigest[normalizeSourceDigest(digest)]
	return uid, ok
}

// Len reports the number of resolvable digests. It is the bounded-cardinality
// signal an operator reads to confirm the source index was populated before
// interpreting a high unresolved-source count.
func (i SourceImageDigestJoinIndex) Len() int {
	return len(i.byDigest)
}

func isOCIDigestSourceFactKind(factKind string) bool {
	switch factKind {
	case facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageDescriptorFactKind:
		return true
	default:
		return false
	}
}

func normalizeSourceDigest(digest string) string {
	return strings.ToLower(strings.TrimSpace(digest))
}

// ociManifestNodeUID returns the canonical node uid for an OCI digest-addressed
// fact payload. It prefers the collector-emitted descriptor_id (the value the
// canonical projector writes as the node uid) and falls back to the same
// deterministic derivation the projector uses (ociDescriptorUID) so the index
// resolves to the node the projector actually committed rather than a fabricated
// id. A payload that carries neither a descriptor_id nor a repository_id+digest
// pair yields "" and is not a join target.
func ociManifestNodeUID(payload map[string]any) string {
	if descriptorID := payloadString(payload, "descriptor_id"); descriptorID != "" {
		return descriptorID
	}
	repositoryID := payloadString(payload, "repository_id")
	digest := payloadString(payload, "digest")
	return deriveOCIDescriptorUID(repositoryID, digest)
}

// deriveOCIDescriptorUID mirrors the registry projector's ociDescriptorUID for
// the prefixed registry path. The projector keys an OCI descriptor node as
// "oci-descriptor://<repository>@<digest>" when the repository_id carries the
// "oci-registry://" prefix; this reproduces that exact form so the derived uid
// matches the written node. A repository_id without the prefix or a blank digest
// is not a derivable identity and returns "".
func deriveOCIDescriptorUID(repositoryID, digest string) string {
	repositoryID = strings.TrimSpace(repositoryID)
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return ""
	}
	if strings.HasPrefix(repositoryID, "oci-registry://") {
		return "oci-descriptor://" + strings.TrimPrefix(repositoryID, "oci-registry://") + "@" + digest
	}
	return ""
}
