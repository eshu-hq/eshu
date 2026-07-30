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
// Manifest/index/tag decode failures do not route through
// partitionDecodeFailures to a second per-fact input_invalid quarantine: those
// same facts are primary graph-identity producers in the projector's canonical
// extractor (oci_registry_canonical.go), which already records the malformed
// input. The reducer's registry index is a secondary cross-source consumer, so
// those three wrappers retain their established skip behavior rather than
// double-counting the dead-letter.
//
// Warning decoding is different. An active warning is a safety declaration
// that blocks destructive retirement. A malformed warning therefore returns a
// classified error and fails the whole handler before publication; silently
// skipping it would convert unknown collector completeness into authoritative
// absence.

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

// decodeOCIRegistryWarning is the fail-closed typed decode seam for active OCI
// warnings consumed by the container-image-identity retirement planner.
func decodeOCIRegistryWarning(env facts.Envelope) (ociregistryv1.Warning, error) {
	warning, err := factschema.DecodeOCIRegistryWarning(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.Warning{}, newFactDecodeError(factschema.FactKindOCIRegistryWarning, err)
	}
	return warning, nil
}
