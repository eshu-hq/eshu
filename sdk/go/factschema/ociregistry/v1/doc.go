// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "oci_registry" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_ociregistry.go).
//
// The oci_registry family fact-kind strings are DOTTED
// ("oci_registry.repository", ...), like the incident family. Seven fact kinds
// live here: Repository (oci_registry.repository), ImageManifest
// (oci_registry.image_manifest), ImageIndex (oci_registry.image_index),
// ImageDescriptor (oci_registry.image_descriptor), TagObservation
// (oci_registry.image_tag_observation), ImageReferrer
// (oci_registry.image_referrer), and Warning (oci_registry.warning).
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers, slices, or
// maps carrying omitempty, so an absent value decodes to nil and stays distinct
// from an observed zero.
//
// These kinds are FULLY TYPED closed structs, not polymorphic envelopes: unlike
// awsv1.Resource / gcpv1.Resource, there is no Attributes pass-through. Every
// payload key a read path consumes is a named field. The nested config/layers
// descriptors on ImageManifest and the manifests list on ImageIndex are typed
// as the shared Descriptor struct (descriptor.go), whose only graph-consumed
// field is Digest.
//
// The required set per kind is exactly the identity/join keys whose ABSENCE
// today produces a broken or empty graph identity, so an absent key becomes a
// classified input_invalid dead-letter (the accuracy fix) while a
// present-but-empty value stays a valid decode the projector's own identity
// gate still drops (byte-identical to today):
//
//   - Repository: RepositoryID
//   - ImageManifest / ImageIndex / ImageDescriptor: RepositoryID, Digest
//   - TagObservation: RepositoryID, Tag, ResolvedDigest
//   - ImageReferrer: RepositoryID, SubjectDigest, ReferrerDigest
//   - Warning: WarningCode
//
// DescriptorID stays OPTIONAL on the digest-addressed kinds because the
// projector synthesizes it from (RepositoryID, Digest) when absent; its absence
// must therefore stay a valid decode.
//
// oci_registry.warning is DEFERRED — typed but not yet consumed. No projector or
// reducer read path decodes it today (design §3.4); the struct, schema,
// fixturepack entry, and registry payload_schema ref exist so the kind is
// contract-complete for conformance and a future consumer, mirroring the gcp
// wave's deferred gcp_image_reference / gcp_tag_observation. It has no
// decode-site conversion, input_invalid regression test, or benchmark because
// there is no read path to convert.
//
// The reducer and projector decode only the latest struct for each kind.
// Version shims for an older schema major live in the parent factschema
// package's decode seam (decodeLatestMajor in decode.go), never in this package
// or in reducer/projector handler code.
package v1
