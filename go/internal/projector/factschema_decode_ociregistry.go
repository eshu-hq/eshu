// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

// This file holds the projector-side decode wrappers for the oci_registry fact
// family. Each wraps the contracts-module Decode* seam and, on a classified
// *factschema.DecodeError (a missing/null required identity field), returns a
// *projectorDecodeError so partitionProjectorDecodeFailures can quarantine the fact
// per-fact rather than the extractor computing a graph identity from an
// empty-string segment. Every oci_registry read site in this package decodes
// through these wrappers — there is no remaining raw payloadString read for a
// typed oci kind.

// decodeOCIRegistryRepository decodes one oci_registry.repository envelope into
// the typed struct through the contracts seam. A missing required field
// (repository_id) yields a self-classifying *projectorDecodeError.
func decodeOCIRegistryRepository(env facts.Envelope) (ociregistryv1.Repository, error) {
	repository, err := factschema.DecodeOCIRegistryRepository(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.Repository{}, newProjectorDecodeError(factschema.FactKindOCIRegistryRepository, err)
	}
	return repository, nil
}

// decodeOCIImageManifest decodes one oci_registry.image_manifest envelope into
// the typed struct. A missing required field (repository_id, digest) yields a
// self-classifying *projectorDecodeError.
func decodeOCIImageManifest(env facts.Envelope) (ociregistryv1.ImageManifest, error) {
	manifest, err := factschema.DecodeOCIImageManifest(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageManifest{}, newProjectorDecodeError(factschema.FactKindOCIImageManifest, err)
	}
	return manifest, nil
}

// decodeOCIImageIndex decodes one oci_registry.image_index envelope into the
// typed struct. A missing required field (repository_id, digest) yields a
// self-classifying *projectorDecodeError.
func decodeOCIImageIndex(env facts.Envelope) (ociregistryv1.ImageIndex, error) {
	index, err := factschema.DecodeOCIImageIndex(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageIndex{}, newProjectorDecodeError(factschema.FactKindOCIImageIndex, err)
	}
	return index, nil
}

// decodeOCIImageDescriptor decodes one oci_registry.image_descriptor envelope
// into the typed struct. A missing required field (repository_id, digest)
// yields a self-classifying *projectorDecodeError.
func decodeOCIImageDescriptor(env facts.Envelope) (ociregistryv1.ImageDescriptor, error) {
	descriptor, err := factschema.DecodeOCIImageDescriptor(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageDescriptor{}, newProjectorDecodeError(factschema.FactKindOCIImageDescriptor, err)
	}
	return descriptor, nil
}

// decodeOCIImageTagObservation decodes one oci_registry.image_tag_observation
// envelope into the typed struct. A missing required field (repository_id, tag,
// resolved_digest) yields a self-classifying *projectorDecodeError.
func decodeOCIImageTagObservation(env facts.Envelope) (ociregistryv1.TagObservation, error) {
	observation, err := factschema.DecodeOCIImageTagObservation(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.TagObservation{}, newProjectorDecodeError(factschema.FactKindOCIImageTagObservation, err)
	}
	return observation, nil
}

// decodeOCIImageReferrer decodes one oci_registry.image_referrer envelope into
// the typed struct. A missing required field (repository_id, subject_digest,
// referrer_digest) yields a self-classifying *projectorDecodeError.
func decodeOCIImageReferrer(env facts.Envelope) (ociregistryv1.ImageReferrer, error) {
	referrer, err := factschema.DecodeOCIImageReferrer(factschemaEnvelope(env))
	if err != nil {
		return ociregistryv1.ImageReferrer{}, newProjectorDecodeError(factschema.FactKindOCIImageReferrer, err)
	}
	return referrer, nil
}

// ociDerefString returns the value a *string points at, or "" when it is nil.
// The typed oci structs carry optional common fields as *string so an absent
// key stays distinct from an observed empty value; the row builders substitute
// "" for an unobserved field, matching the pre-typing payloadString("") behavior.
func ociDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ociDerefInt64 returns the value a *int64 points at, or 0 when it is nil.
func ociDerefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// ociDerefBool returns the value a *bool points at, or false when it is nil.
func ociDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// ociDescriptorDigest returns the digest of a nested *Descriptor, or "" when the
// descriptor or its digest is absent. It replaces the pre-typing
// ociDescriptorMapDigest raw-map read for the manifest's config descriptor.
func ociDescriptorDigest(descriptor *ociregistryv1.Descriptor) string {
	if descriptor == nil {
		return ""
	}
	return ociDerefString(descriptor.Digest)
}

// ociDescriptorSliceDigests returns the deduplicated, sorted set of non-empty
// digests from a typed []Descriptor (a manifest's layers or an index's
// manifests), preserving the pre-typing ociDescriptorListDigests behavior over
// the typed structs. It returns nil for an empty result so the row field stays
// nil rather than an empty slice, byte-identical to the old raw-map read.
func ociDescriptorSliceDigests(descriptors []ociregistryv1.Descriptor) []string {
	if len(descriptors) == 0 {
		return nil
	}
	digests := make([]string, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for i := range descriptors {
		digest := strings.TrimSpace(ociDerefString(descriptors[i].Digest))
		if digest == "" {
			continue
		}
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		digests = append(digests, digest)
	}
	if len(digests) == 0 {
		return nil
	}
	sort.Strings(digests)
	return digests
}

// ociUniqueSortedAnchors returns the trimmed, non-empty, sorted correlation
// anchors from a typed []string, preserving the pre-typing ociCorrelationAnchors
// behavior. It returns nil for an empty result so the row field stays nil,
// byte-identical to the old raw read.
func ociUniqueSortedAnchors(anchors []string) []string {
	if len(anchors) == 0 {
		return nil
	}
	out := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		if trimmed := strings.TrimSpace(anchor); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
