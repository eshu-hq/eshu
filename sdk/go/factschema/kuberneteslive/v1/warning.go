// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for the
// "kubernetes_live.warning" fact kind: one non-fatal Kubernetes live
// collection warning (a forbidden resource, partial list, skipped secret,
// unsupported API, or ambiguous selector). A warning reports a capability
// gap; it never asserts that a resource set is complete.
//
// The required set matches the collector emitter
// (kuberneteslive.NewWarningEnvelope), which rejects a blank Reason and a
// blank ClusterID before the envelope is ever built, and the reducer's own
// ingest gate (kubernetesCorrelationIndex.ingestWarning), which already drops
// a warning missing Reason rather than recording an empty-string reason.
// Reason is required for that identity/accuracy reason. ClusterID is
// required too: the emitter fails closed on a blank cluster id, so a payload
// missing it is a collector-boundary defect the decode seam should surface
// as a classified error rather than silently accept.
type Warning struct {
	// Reason is the warning reason token. Required — the emitter rejects a
	// blank reason, and the reducer's ingest gate already drops a warning
	// missing this field rather than recording an empty-string reason.
	Reason string `json:"reason"`

	// ClusterID is the operator-declared cluster identity. Required — the
	// emitter rejects a blank cluster id before the envelope is built.
	ClusterID string `json:"cluster_id"`

	// ResourceScope is the warning's resource scope (for example a
	// group/version/resource or namespace token). Optional: the emitter
	// always writes it, but it may legitimately be empty for a cluster-wide
	// warning with no narrower scope.
	ResourceScope *string `json:"resource_scope,omitempty"`

	// Message is the redacted human-readable warning message. Optional: the
	// emitter sanitizes the message but may still produce an empty string.
	Message *string `json:"message,omitempty"`

	// CorrelationAnchors are redaction-safe join anchors (the cluster id) the
	// correlation reducer domain may use for name-only join resolution.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
