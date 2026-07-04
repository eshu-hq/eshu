// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "azure" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_azure.go).
//
// Four fact kinds live here: CloudResource (azure_cloud_resource),
// CloudRelationship (azure_cloud_relationship), DNSRecord (azure_dns_record),
// and CollectionWarning (azure_collection_warning). Each struct's required
// fields are non-pointer with no omitempty tag; the decode seam rejects a
// payload that omits one, or supplies an explicit JSON null for one, with a
// classified ClassificationInputInvalid error naming the field, never a
// zero-value struct. Optional fields are pointers, slices, or maps carrying
// omitempty, so an absent value decodes to nil and stays distinct from an
// observed zero.
//
// The Azure family has eight fact kinds in total. This package deliberately
// types only the four whose read-side consumer is either an Azure-specific
// reducer handler converted in the same Contract System v1 wave that added
// these structs (azure_cloud_resource, azure_cloud_relationship) or genuinely
// consumer-less today (azure_dns_record, azure_collection_warning — validated
// forward by the drift tests). The other four (azure_tag_observation,
// azure_identity_observation, azure_resource_change, azure_image_reference)
// are intentionally left untyped: their sole read-side consumers are either a
// shared cross-provider surface (cloud-tag-evidence, container-image-identity)
// or an Azure-specific storage loader not converted in this wave, so typing
// them here would create a Decode* the real read path never calls — a hollow
// contract where validation silently never happens. They migrate WITH the
// surface that reads them, in a later change, per Contract System v1 §7.
//
// CloudResource and CloudRelationship are polymorphic generic envelopes: one
// fact kind carries every Azure ARM resource type or relationship verb, so
// each struct types and validates only the shared identity contract and the
// common fields multiple consumers read. Every remaining, service- or
// verb-specific payload key passes through untyped in the struct's
// Attributes field. Unlike the aws family's nested "attributes" object, the
// Azure collector emitter (go/internal/collector/azurecloud) writes its
// service-specific fields FLAT at the top level of the payload (for example
// "kind", "sku_class", "tags", "extension"), so Attributes here captures
// those flat keys directly — there is no nested "attributes" object to
// unwrap.
//
// DNSRecord and CollectionWarning are each scoped to one fact kind with a
// known, closed field set and carry no Attributes pass-through: the collector
// fingerprints or redacts every sensitive field before emission, so the full
// payload shape is already known and stable.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in
// reducer handler code.
package v1
