package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func ociManifestEnvelope(payload map[string]any, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactKind:    facts.OCIImageManifestFactKind,
		FactID:      "fact-" + anyToString(payload["digest"]),
		IsTombstone: tombstone,
		Payload:     payload,
	}
}

func sampleManifestPayload(repositoryID, descriptorID, digest string) map[string]any {
	return map[string]any{
		"repository_id": repositoryID,
		"descriptor_id": descriptorID,
		"digest":        digest,
	}
}

func TestBuildSourceImageDigestJoinIndexEmptyInput(t *testing.T) {
	t.Parallel()

	index := BuildSourceImageDigestJoinIndex(nil)
	if _, ok := index.ResolveDigest("sha256:abc"); ok {
		t.Fatal("empty index must resolve nothing")
	}
}

func TestSourceImageDigestJoinIndexResolvesDigestToManifestNodeUID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ociManifestEnvelope(sampleManifestPayload(
			"oci-registry://registry.example.com/checkout",
			"oci-descriptor://registry.example.com/checkout@sha256:abc",
			"sha256:abc",
		), false),
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	uid, ok := index.ResolveDigest("sha256:abc")
	if !ok {
		t.Fatal("expected the source digest to resolve to a manifest node uid")
	}
	if want := "oci-descriptor://registry.example.com/checkout@sha256:abc"; uid != want {
		t.Fatalf("uid = %q, want %q (the ContainerImage/OCI manifest node uid)", uid, want)
	}
}

func TestSourceImageDigestJoinIndexDerivesUIDWhenDescriptorIDAbsent(t *testing.T) {
	t.Parallel()

	// A manifest fact that did not carry an explicit descriptor_id must resolve to
	// the same uid the canonical projector derives, so the join hits the node the
	// projector actually wrote rather than a fabricated id.
	envelopes := []facts.Envelope{
		ociManifestEnvelope(map[string]any{
			"repository_id": "oci-registry://registry.example.com/checkout",
			"digest":        "sha256:def",
		}, false),
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	uid, ok := index.ResolveDigest("sha256:def")
	if !ok {
		t.Fatal("expected the source digest to resolve via the derived uid")
	}
	if want := "oci-descriptor://registry.example.com/checkout@sha256:def"; uid != want {
		t.Fatalf("derived uid = %q, want %q", uid, want)
	}
}

func TestSourceImageDigestJoinIndexUnresolvableDigest(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ociManifestEnvelope(sampleManifestPayload(
			"oci-registry://registry.example.com/checkout",
			"oci-descriptor://registry.example.com/checkout@sha256:abc",
			"sha256:abc",
		), false),
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	if uid, ok := index.ResolveDigest("sha256:missing"); ok {
		t.Fatalf("unknown digest must not resolve, got uid %q", uid)
	}
}

func TestSourceImageDigestJoinIndexSkipsTombstonedManifest(t *testing.T) {
	t.Parallel()

	// A tombstoned manifest is a removed source. It must not resolve as a live
	// node uid; an edge resolving to it would point at a node that no longer
	// exists.
	envelopes := []facts.Envelope{
		ociManifestEnvelope(sampleManifestPayload(
			"oci-registry://registry.example.com/checkout",
			"oci-descriptor://registry.example.com/checkout@sha256:tomb",
			"sha256:tomb",
		), true),
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	if _, ok := index.ResolveDigest("sha256:tomb"); ok {
		t.Fatal("a tombstoned manifest must not resolve to a live node uid")
	}
}

func TestSourceImageDigestJoinIndexActiveOverridesTombstone(t *testing.T) {
	t.Parallel()

	// A digest seen active anywhere is not removed; an active observation must
	// override a tombstone for the same digest regardless of envelope order.
	active := ociManifestEnvelope(sampleManifestPayload(
		"oci-registry://registry.example.com/checkout",
		"oci-descriptor://registry.example.com/checkout@sha256:abc",
		"sha256:abc",
	), false)
	tombstone := ociManifestEnvelope(sampleManifestPayload(
		"oci-registry://registry.example.com/checkout",
		"oci-descriptor://registry.example.com/checkout@sha256:abc",
		"sha256:abc",
	), true)

	index := BuildSourceImageDigestJoinIndex([]facts.Envelope{tombstone, active})
	if _, ok := index.ResolveDigest("sha256:abc"); !ok {
		t.Fatal("an active observation must override a tombstone for the same digest")
	}
}

func TestSourceImageDigestJoinIndexSkipsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// Missing digest: not a node.
		ociManifestEnvelope(map[string]any{
			"repository_id": "oci-registry://registry.example.com/checkout",
			"descriptor_id": "oci-descriptor://registry.example.com/checkout@",
		}, false),
		// Missing repository_id and descriptor_id: cannot derive a uid.
		ociManifestEnvelope(map[string]any{
			"digest": "sha256:orphan",
		}, false),
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	if index.Len() != 0 {
		t.Fatalf("index length = %d, want 0 for incomplete identities", index.Len())
	}
}

func TestSourceImageDigestJoinIndexIngestsImageIndexAndDescriptorFacts(t *testing.T) {
	t.Parallel()

	// The source endpoint may be a multi-arch image index or a reusable
	// descriptor, not only a manifest. All three OCI digest-addressed node kinds
	// resolve by digest to the canonical node uid.
	envelopes := []facts.Envelope{
		{
			FactKind: facts.OCIImageIndexFactKind,
			FactID:   "fact-index",
			Payload: sampleManifestPayload(
				"oci-registry://registry.example.com/checkout",
				"oci-descriptor://registry.example.com/checkout@sha256:index",
				"sha256:index",
			),
		},
		{
			FactKind: facts.OCIImageDescriptorFactKind,
			FactID:   "fact-descriptor",
			Payload: sampleManifestPayload(
				"oci-registry://registry.example.com/checkout",
				"oci-descriptor://registry.example.com/checkout@sha256:desc",
				"sha256:desc",
			),
		},
	}

	index := BuildSourceImageDigestJoinIndex(envelopes)
	if _, ok := index.ResolveDigest("sha256:index"); !ok {
		t.Fatal("an image index digest must resolve to its node uid")
	}
	if _, ok := index.ResolveDigest("sha256:desc"); !ok {
		t.Fatal("a reusable descriptor digest must resolve to its node uid")
	}
}
