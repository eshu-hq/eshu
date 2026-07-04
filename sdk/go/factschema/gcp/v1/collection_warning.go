// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// CollectionWarning is the schema-version-1 typed payload for the
// "gcp_collection_warning" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set matches the collector emitter
// (gcpcloud.NewCollectionWarningEnvelope), which validates WarningKind and
// Outcome against bounded closed vocabularies (ValidWarningKind, ValidOutcome)
// before ever reaching the envelope builder — so both are always non-empty on
// a valid fact, and either being absent means the collector's own validation
// was bypassed. Reason, Retryable, and HiddenCount are optional descriptive
// fields the collector always emits but that carry no identity meaning.
type CollectionWarning struct {
	// WarningKind is the bounded warning category (for example a specific
	// permission-hidden or quota class). Required.
	WarningKind string `json:"warning_kind"`

	// Outcome is the bounded outcome classification (partial, unsupported,
	// stale, permission-hidden, quota, or redaction). Required.
	Outcome string `json:"outcome"`

	// Reason is a free-text explanation of the warning. Optional: always
	// emitted but may be empty.
	Reason *string `json:"reason,omitempty"`

	// Retryable reports whether the collector expects retrying to resolve the
	// warning. Optional pointer so nil (unreported) stays distinct from an
	// observed false.
	Retryable *bool `json:"retryable,omitempty"`

	// HiddenCount is the number of resources the warning's outcome hid from
	// collection, when applicable. Optional pointer so nil stays distinct from
	// an observed zero.
	HiddenCount *int64 `json:"hidden_count,omitempty"`
}
