// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestSourceImageDigestJoinIndexResolveNodeReturnsLabelPerKind(t *testing.T) {
	t.Parallel()

	// The edge MATCH must anchor the source endpoint on the exact uid-indexed
	// label the projector wrote, and the three OCI digest-addressed node kinds
	// carry distinct labels (OciImageManifest / OciImageIndex / OciImageDescriptor).
	// ResolveDigestNode therefore returns the node label alongside the uid so the
	// edge writer can group its MATCH by label rather than guessing a single label
	// that would miss index or descriptor source nodes.
	envelopes := []facts.Envelope{
		ociManifestEnvelope(sampleManifestPayload(
			"oci-registry://registry.example.com/checkout",
			"oci-descriptor://registry.example.com/checkout@sha256:man",
			"sha256:man",
		), false),
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
	for _, tc := range []struct {
		digest    string
		wantLabel string
	}{
		{"sha256:man", sourceImageNodeLabelManifest},
		{"sha256:index", sourceImageNodeLabelIndex},
		{"sha256:desc", sourceImageNodeLabelDescriptor},
	} {
		node, ok := index.ResolveDigestNode(tc.digest)
		if !ok {
			t.Fatalf("digest %q must resolve to a source node", tc.digest)
		}
		if node.Label != tc.wantLabel {
			t.Fatalf("digest %q label = %q, want %q", tc.digest, node.Label, tc.wantLabel)
		}
		if node.UID == "" {
			t.Fatalf("digest %q resolved an empty uid", tc.digest)
		}
		// ResolveDigest stays compatible: same uid, no label.
		uid, okLegacy := index.ResolveDigest(tc.digest)
		if !okLegacy || uid != node.UID {
			t.Fatalf("ResolveDigest %q = (%q,%v), want (%q,true)", tc.digest, uid, okLegacy, node.UID)
		}
	}
}
