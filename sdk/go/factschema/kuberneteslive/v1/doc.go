// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "kubernetes_live" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_kuberneteslive.go).
//
// Four fact kinds live here: PodTemplate (kubernetes_live.pod_template),
// Relationship (kubernetes_live.relationship), Warning
// (kubernetes_live.warning), and Namespace (kubernetes_live.namespace). Each
// struct's required fields are non-pointer
// with no omitempty tag; the decode seam rejects a payload that omits one, or
// supplies an explicit JSON null for one, with a classified
// ClassificationInputInvalid error naming the field, never a zero-value
// struct. Optional fields are pointers, slices, or maps carrying omitempty,
// so an absent value decodes to nil and stays distinct from an observed
// zero.
//
// Every struct is fully typed with a known field set; none carries an
// untyped Attributes pass-through, unlike the polymorphic AWS/GCP
// gcp_cloud_resource/gcp_cloud_relationship kinds. The kubernetes_live family
// is not a polymorphic generic envelope: each fact kind describes one fixed
// observation shape (a pod template, a directed object relationship, a
// collection warning, or a namespace's label evidence). Each struct models
// the reducer-consumed field set as
// named fields rather than an opaque map; the generated schema is open
// (additionalProperties), so boundary and context keys the collector also
// emits — for example collector_instance_id — are permitted and ignored on
// decode rather than modeled here.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
