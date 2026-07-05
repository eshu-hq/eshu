// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "work_item" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_workitem.go).
//
// The directory name (workitem/v1) mirrors the fact-kind namespace
// (work_item), the same convention incident/v1 follows for the "incident"
// namespace — NOT the collector package name (jira). Nine fact kinds live
// here, all emitted by the Jira collector (go/internal/collector/jira):
//
//   - WorkItemRecord (work_item.record)
//   - WorkItemTransition (work_item.transition)
//   - WorkItemExternalLink (work_item.external_link)
//   - WorkItemProjectMetadata (work_item.project_metadata)
//   - WorkItemIssueTypeMetadata (work_item.issue_type_metadata)
//   - WorkItemStatusMetadata (work_item.status_metadata)
//   - WorkItemWorkflowMetadata (work_item.workflow_metadata)
//   - WorkItemFieldMetadata (work_item.field_metadata)
//   - WorkItemMetadataWarning (work_item.metadata_warning)
//
// These are DOTTED fact kinds, matching the incident/kubernetes_live/
// oci_registry/package_registry/sbom_attestation convention: the wire strings
// carry namespace dots (for example "work_item.record") because that is what
// go/internal/facts.WorkItemRecordFactKind and its siblings already emit. This
// package matches them byte-for-byte and never invents or renames the
// namespace. The schema filename is the dotted kind plus ".v1.schema.json"
// (work_item.record.v1.schema.json).
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers, slices, or
// maps carrying omitempty, so an absent value decodes to nil and stays
// distinct from an observed zero.
//
// The required set of each struct is grounded in the actual collector
// emitters (go/internal/collector/jira/envelope.go and envelope_metadata.go):
// a field is required only where the emitter's identity guard makes it
// unconditional. Every kind requires "provider" (stamped on every payload).
// WorkItemRecord additionally requires "provider_work_item_id" and
// "work_item_key" (the emitter rejects a blank issue id or key).
// WorkItemTransition, WorkItemExternalLink, and WorkItemProjectMetadata do NOT
// require their natural identity fields beyond provider, because the
// emitter's guard accepts more than one alternate anchor (external link:
// id/global_id/url fingerprint; project metadata: id OR key) or does not
// validate the field at all (transition: issue id/key come from unvalidated
// source fields). WorkItemIssueTypeMetadata, WorkItemStatusMetadata, and
// WorkItemWorkflowMetadata additionally require their own id field
// (issue_type_id, status_id, workflow_id respectively), each guarded
// non-blank by its emitter. WorkItemFieldMetadata is the one exception where
// the guarded Go-level field id is NOT promoted to a required payload key,
// because the payload's own "field_id" key is always emitted as the redacted
// empty string — only "field_id_fingerprint" carries the derived identity, so
// requiring "field_id" would dead-letter every valid fact. WorkItemMetadata-
// Warning requires "metadata_type" and "reason" (the emitter rejects either
// blank).
//
// WorkItemWorkflowMetadata carries two nested typed lists (Statuses,
// Transitions), the Wave-3 nested-struct support pattern also used by other
// migrated families' array fields.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer/query
// handler code.
package v1
