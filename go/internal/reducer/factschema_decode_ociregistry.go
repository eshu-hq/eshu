// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

// This file holds the reducer-side decode wrappers for the oci_registry fact
// family, used by the container-image-identity domain's registry-index builder
// (container_image_identity_registry.go). Each wraps the contracts-module
// Decode* seam.
//
// Unlike the AWS/GCP/Azure reducer decode wrappers, these do NOT route a decode
// failure through partitionDecodeFailures to a per-fact input_invalid
// quarantine: the SAME oci_registry facts are the PRIMARY graph-identity
// producers in the projector's canonical extractor (oci_registry_canonical.go),
// which already quarantines a malformed fact as input_invalid and increments
// eshu_dp_projector_input_invalid_facts_total. The reducer's registry index is a
// SECONDARY cross-source consumer (it correlates already-projected digest/tag
// observations to container image references); double-recording the same
// malformed fact here would over-count the dead-letter. So a decode error here
// is a SKIP that matches the pre-typing behavior (an incomplete observation was
// already dropped with ok=false), while the operator-facing dead-letter is
// emitted once, at the projector. The typed decode still removes the raw
// payloadStr reads for the typed identity fields, so the field contract is
// single-sourced.

// decodeOCIImageManifestForIndex decodes an oci_registry.image_manifest or
// oci_registry.image_index envelope's typed fields for the container-image
// registry index. It returns ok=false on a decode error (the projector already
// dead-lettered the malformed fact). Digest-identity emptiness is enforced by
// the caller (ociDigestObservation), not here.
func decodeOCIImageManifestForIndex(env facts.Envelope) (ociregistryv1.ImageManifest, bool) {
	manifest, err := factschema.DecodeOCIImageManifest(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageManifest{}, false
	}
	return manifest, true
}

// decodeOCIImageIndexForIndex decodes an oci_registry.image_index envelope's
// typed fields for the container-image registry index. It returns ok=false on a
// decode error (the projector already dead-lettered the malformed fact).
func decodeOCIImageIndexForIndex(env facts.Envelope) (ociregistryv1.ImageIndex, bool) {
	index, err := factschema.DecodeOCIImageIndex(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageIndex{}, false
	}
	return index, true
}

// decodeOCIImageTagObservationForIndex decodes an
// oci_registry.image_tag_observation envelope's typed fields for the
// container-image registry index. It returns ok=false on a decode error (the
// projector already dead-lettered the malformed fact).
func decodeOCIImageTagObservationForIndex(env facts.Envelope) (ociregistryv1.TagObservation, bool) {
	observation, err := factschema.DecodeOCIImageTagObservation(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.TagObservation{}, false
	}
	return observation, true
}
