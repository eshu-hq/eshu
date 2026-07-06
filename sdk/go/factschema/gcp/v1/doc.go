// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "gcp" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_gcp.go).
//
// Five fact kinds live here: Resource (gcp_cloud_resource), Relationship
// (gcp_cloud_relationship), CollectionWarning (gcp_collection_warning),
// DNSRecord (gcp_dns_record), and IAMPolicyObservation
// (gcp_iam_policy_observation). Each struct's required fields are
// non-pointer with no omitempty tag; the decode seam rejects a payload that
// omits one, or supplies an explicit JSON null for one, with a classified
// ClassificationInputInvalid error naming the field, never a zero-value
// struct. Optional fields are pointers, slices, or maps carrying omitempty,
// so an absent value decodes to nil and stays distinct from an observed
// zero.
//
// Resource and Relationship are polymorphic generic envelopes mirroring
// awsv1.Resource / awsv1.Relationship: one fact kind carries every GCP asset
// type or relationship type, so each struct types and validates only the
// shared identity contract and the common fields the node/edge projector
// reads. Every remaining payload key passes through untyped in the struct's
// Attributes field with JSON type fidelity preserved by a custom
// UnmarshalJSON/MarshalJSON pair. A consumer reaches a nested attribute
// through the decoded struct with the reducer's
// payloadAttributes(resource.Attributes) helper — never
// env.Payload["attributes"][key] directly.
//
// CollectionWarning, DNSRecord, and IAMPolicyObservation are each scoped to
// one fact kind with a known field set and carry no Attributes pass-through.
//
// gcp_image_reference and gcp_tag_observation are deliberately NOT typed
// here: each kind's sole read-side reducer/storage consumer is a shared
// cross-provider surface (container_image_identity for image references,
// cloud_tag_evidence for tag observations) that reads AWS/Azure/GCP kinds
// together and still reads them raw. Typing one provider's kind ahead of that
// shared surface would be a hollow contract — the decode seam would never be
// called by the real read path — and would asymmetrically type GCP while its
// AWS/Azure siblings stay raw. These two kinds migrate WITH their
// cross-provider consumer, not in this per-cloud wave, matching how the
// AWS cloud support now types image references; tag observations still migrate
// with their shared cross-provider consumer.
//
// GCPCloudResourceSchemaVersion (go/internal/facts.gcp.go) is pinned at
// 1.1.0, one minor ahead of the rest of this family's 1.0.0 kinds: 1.1.0
// added the bounded typed-depth Attributes contract as an additive,
// backward-compatible bump. The decode seam still dispatches on the
// schema-version MAJOR only, so this is a version-artifact detail, not a
// second decode path.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in
// reducer handler code.
//
// gcp_iam_principal, gcp_iam_trust_policy, and gcp_iam_permission_policy are
// OUT OF SCOPE for this package: they belong to the secrets_iam fact family
// (go/internal/facts/secrets_iam.go), a distinct family boundary from the GCP
// cloud-inventory family this package types.
package v1
