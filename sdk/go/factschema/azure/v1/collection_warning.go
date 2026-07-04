// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// CollectionWarning is the schema-version-1 typed payload for the
// "azure_collection_warning" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A fully typed, CLOSED schema: the collector emitter
// (azurecloud.NewWarningEnvelope) reports an explicit partial,
// permission-hidden, truncation, throttle, fallback, stale, unsupported, or
// redaction coverage outcome as evidence — the payload's full shape is
// already known. This is how the Azure collector reports incomplete
// coverage as visible evidence instead of silent success.
//
// Required fields mirror what the emitter validates: WarningKind (rejected
// if blank) and Outcome (the emitter defaults a blank outcome to "partial"
// before emission, so it is always present once decode succeeds).
type CollectionWarning struct {
	// WarningKind is the bounded warning taxonomy value (for example
	// partial_scope, permission_hidden, result_truncated, throttled,
	// fallback_skipped, stale, unsupported, or redaction). Required: the
	// emitter rejects a blank value.
	WarningKind string `json:"warning_kind"`

	// Outcome is the bounded resolution state (partial, stale, unavailable,
	// or unsupported). Required: the emitter defaults a blank value to
	// "partial" before emission.
	Outcome string `json:"outcome"`

	// ResourceFamily buckets the resource provider namespace the warning
	// applies to. Optional metadata.
	ResourceFamily *string `json:"resource_family,omitempty"`

	// Retryable reports whether the condition may resolve on retry. Optional
	// pointer so nil (unreported) stays distinct from an observed false.
	Retryable *bool `json:"retryable,omitempty"`

	// HiddenResourceCount counts resources the principal could not read in
	// the scope, when known. Optional pointer so nil (unreported) stays
	// distinct from an observed zero.
	HiddenResourceCount *int32 `json:"hidden_resource_count,omitempty"`

	// Message is an operator-facing detail, sanitized before persistence.
	// Optional: may be blank when the warning carries no additional detail.
	Message *string `json:"message,omitempty"`
}
