// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

// DecodeOCIRegistryRepository decodes env.Payload into the latest
// ociregistryv1.Repository struct for the "oci_registry.repository" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (the projector's canonical extractors) receive either the decoded struct or
// a classified *DecodeError; they must never substitute a zero-value struct on
// error.
func DecodeOCIRegistryRepository(env Envelope) (ociregistryv1.Repository, error) {
	return decodeLatestMajor[ociregistryv1.Repository](FactKindOCIRegistryRepository, env)
}

// EncodeOCIRegistryRepository marshals an ociregistryv1.Repository into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIRegistryRepository for schema-version-1 payloads.
func EncodeOCIRegistryRepository(repository ociregistryv1.Repository) (map[string]any, error) {
	return encodeDirectPayload(repository)
}

// DecodeOCIImageManifest decodes env.Payload into the latest
// ociregistryv1.ImageManifest struct for the "oci_registry.image_manifest"
// fact kind. See DecodeOCIRegistryRepository for the dispatch and error
// contract.
func DecodeOCIImageManifest(env Envelope) (ociregistryv1.ImageManifest, error) {
	return decodeLatestMajor[ociregistryv1.ImageManifest](FactKindOCIImageManifest, env)
}

// EncodeOCIImageManifest marshals an ociregistryv1.ImageManifest into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIImageManifest for schema-version-1 payloads.
func EncodeOCIImageManifest(manifest ociregistryv1.ImageManifest) (map[string]any, error) {
	payload := map[string]any{
		"repository_id": manifest.RepositoryID,
		"digest":        manifest.Digest,
	}
	addStringPtr(payload, "descriptor_id", manifest.DescriptorID)
	addStringPtr(payload, "media_type", manifest.MediaType)
	addInt64Ptr(payload, "size_bytes", manifest.SizeBytes)
	addStringPtr(payload, "artifact_type", manifest.ArtifactType)
	addStringPtr(payload, "source_tag", manifest.SourceTag)
	if manifest.Config != nil {
		payload["config"] = encodeOCIDescriptor(*manifest.Config)
	}
	addStringMap(payload, "config_labels", manifest.ConfigLabels)
	if len(manifest.Layers) > 0 {
		payload["layers"] = encodeOCIDescriptors(manifest.Layers)
	}
	addStringSlice(payload, "correlation_anchors", manifest.CorrelationAnchors)
	addStringPtr(payload, "collector_instance_id", manifest.CollectorInstanceID)
	return payload, nil
}

// DecodeOCIImageIndex decodes env.Payload into the latest
// ociregistryv1.ImageIndex struct for the "oci_registry.image_index" fact
// kind. See DecodeOCIRegistryRepository for the dispatch and error contract.
func DecodeOCIImageIndex(env Envelope) (ociregistryv1.ImageIndex, error) {
	return decodeLatestMajor[ociregistryv1.ImageIndex](FactKindOCIImageIndex, env)
}

// EncodeOCIImageIndex marshals an ociregistryv1.ImageIndex into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIImageIndex for schema-version-1 payloads.
func EncodeOCIImageIndex(index ociregistryv1.ImageIndex) (map[string]any, error) {
	return encodeDirectPayload(index)
}

// DecodeOCIImageDescriptor decodes env.Payload into the latest
// ociregistryv1.ImageDescriptor struct for the
// "oci_registry.image_descriptor" fact kind. See DecodeOCIRegistryRepository
// for the dispatch and error contract.
func DecodeOCIImageDescriptor(env Envelope) (ociregistryv1.ImageDescriptor, error) {
	return decodeLatestMajor[ociregistryv1.ImageDescriptor](FactKindOCIImageDescriptor, env)
}

// EncodeOCIImageDescriptor marshals an ociregistryv1.ImageDescriptor into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIImageDescriptor for schema-version-1 payloads.
func EncodeOCIImageDescriptor(descriptor ociregistryv1.ImageDescriptor) (map[string]any, error) {
	return encodeDirectPayload(descriptor)
}

// DecodeOCIImageTagObservation decodes env.Payload into the latest
// ociregistryv1.TagObservation struct for the
// "oci_registry.image_tag_observation" fact kind. See
// DecodeOCIRegistryRepository for the dispatch and error contract.
func DecodeOCIImageTagObservation(env Envelope) (ociregistryv1.TagObservation, error) {
	return decodeLatestMajor[ociregistryv1.TagObservation](FactKindOCIImageTagObservation, env)
}

// EncodeOCIImageTagObservation marshals an ociregistryv1.TagObservation into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIImageTagObservation for schema-version-1 payloads.
func EncodeOCIImageTagObservation(observation ociregistryv1.TagObservation) (map[string]any, error) {
	return encodeDirectPayload(observation)
}

// DecodeOCIImageReferrer decodes env.Payload into the latest
// ociregistryv1.ImageReferrer struct for the "oci_registry.image_referrer"
// fact kind. See DecodeOCIRegistryRepository for the dispatch and error
// contract.
func DecodeOCIImageReferrer(env Envelope) (ociregistryv1.ImageReferrer, error) {
	return decodeLatestMajor[ociregistryv1.ImageReferrer](FactKindOCIImageReferrer, env)
}

// EncodeOCIImageReferrer marshals an ociregistryv1.ImageReferrer into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIImageReferrer for schema-version-1 payloads.
func EncodeOCIImageReferrer(referrer ociregistryv1.ImageReferrer) (map[string]any, error) {
	return encodeDirectPayload(referrer)
}

// DecodeOCIRegistryWarning decodes env.Payload into the latest
// ociregistryv1.Warning struct for the "oci_registry.warning" fact kind. See
// DecodeOCIRegistryRepository for the dispatch and error contract.
//
// oci_registry.warning is a DEFERRED, typed-but-not-yet-consumed kind: no
// projector or reducer read path calls this decode function today (design
// §3.4). It exists so the kind is contract-complete for conformance and a
// future consumer, mirroring the gcp wave's deferred image_reference /
// tag_observation.
func DecodeOCIRegistryWarning(env Envelope) (ociregistryv1.Warning, error) {
	return decodeLatestMajor[ociregistryv1.Warning](FactKindOCIRegistryWarning, env)
}

// EncodeOCIRegistryWarning marshals an ociregistryv1.Warning into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeOCIRegistryWarning for schema-version-1 payloads.
func EncodeOCIRegistryWarning(warning ociregistryv1.Warning) (map[string]any, error) {
	return encodeDirectPayload(warning)
}

func encodeOCIDescriptors(descriptors []ociregistryv1.Descriptor) []map[string]any {
	out := make([]map[string]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, encodeOCIDescriptor(descriptor))
	}
	return out
}

func encodeOCIDescriptor(descriptor ociregistryv1.Descriptor) map[string]any {
	payload := make(map[string]any)
	addStringPtr(payload, "digest", descriptor.Digest)
	addStringPtr(payload, "media_type", descriptor.MediaType)
	addInt64Ptr(payload, "size_bytes", descriptor.SizeBytes)
	addStringPtr(payload, "artifact_type", descriptor.ArtifactType)
	addStringMap(payload, "annotations", descriptor.Annotations)
	addStringMap(payload, "platform", descriptor.Platform)
	return payload
}
