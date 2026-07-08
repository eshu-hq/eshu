// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// This file holds the evidence-state classification for the work_item evidence
// read surface: mapping one decoded fact to its truth label, validating a
// caller-supplied state, summarizing the states in a page, and shaping the
// per-state span counters. The evidence-state constants themselves live with
// the rest of the read-surface contract in work_item_evidence.go.

// workItemEvidenceState classifies one fact row into its evidence state. The
// order of checks is load-bearing: a work_item.metadata_warning is a
// metadata-collection warning for the whole kind, so it takes the distinct
// metadata_warning state ahead of every payload-driven check below — a warning
// with reason=permission_hidden carries failure_class=permission_hidden, which
// the generic check would otherwise map to the record-level permission_hidden
// state and conflate "collection blocked" with "record hidden". The specific
// reason stays in the row's WarningReason field.
func workItemEvidenceState(fact workItemEvidenceFactRow) string {
	payload := fact.Payload
	if fact.FactKind == "work_item.metadata_warning" {
		return WorkItemEvidenceStateMetadataWarning
	}
	if state := strings.TrimSpace(StringVal(payload, "evidence_state")); knownWorkItemEvidenceState(state) {
		return state
	}
	if BoolVal(payload, "permission_hidden") ||
		StringVal(payload, "failure_class") == "permission_hidden" ||
		StringVal(payload, "visibility_state") == "permission_hidden" {
		return WorkItemEvidenceStatePermissionHidden
	}
	if StringVal(payload, "source_freshness") == "stale" ||
		StringVal(payload, "freshness_state") == "stale" {
		return WorkItemEvidenceStateStaleEvidence
	}
	if fact.FactKind == "work_item.external_link" {
		state := strings.TrimSpace(StringVal(payload, "provider_support_state"))
		switch {
		case strings.Contains(state, "unsupported"):
			return WorkItemEvidenceStateUnsupportedLinkType
		case strings.Contains(state, "rejected"):
			return WorkItemEvidenceStateRejectedUnsafePayload
		}
	}
	if StringVal(payload, "warning_reason") == "rejected_unsafe_payload" {
		return WorkItemEvidenceStateRejectedUnsafePayload
	}
	return WorkItemEvidenceStateExactProviderFact
}

// knownWorkItemEvidenceState reports whether a state token is one of the
// bounded evidence states the read surface promotes.
func knownWorkItemEvidenceState(state string) bool {
	return slices.Contains([]string{
		WorkItemEvidenceStateExactProviderFact,
		WorkItemEvidenceStateUnsupportedLinkType,
		WorkItemEvidenceStateMissingEvidence,
		WorkItemEvidenceStateStaleEvidence,
		WorkItemEvidenceStatePermissionHidden,
		WorkItemEvidenceStateRejectedUnsafePayload,
		WorkItemEvidenceStateMetadataWarning,
	}, state)
}

// summarizeWorkItemEvidenceStates returns the sorted distinct evidence states
// present in a page of rows, or the missing-evidence state for an empty page.
func summarizeWorkItemEvidenceStates(rows []WorkItemEvidenceRow) []string {
	if len(rows) == 0 {
		return []string{WorkItemEvidenceStateMissingEvidence}
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = WorkItemEvidenceStateExactProviderFact
		}
		seen[state] = struct{}{}
	}
	return setToSortedSlice(seen)
}

// workItemEvidenceSpanAttributes shapes the bounded per-state counters an
// operator reads on the evidence-list span. It counts the concern states
// (stale, permission-hidden, rejected-unsafe-payload, unsupported-link-type,
// metadata-warning) plus result/missing counts; exact_provider_fact is the
// baseline and is not broken out.
func workItemEvidenceSpanAttributes(rows []WorkItemEvidenceRow, truncated bool) []attribute.KeyValue {
	counts := map[string]int{
		WorkItemEvidenceStateStaleEvidence:         0,
		WorkItemEvidenceStatePermissionHidden:      0,
		WorkItemEvidenceStateRejectedUnsafePayload: 0,
		WorkItemEvidenceStateUnsupportedLinkType:   0,
		WorkItemEvidenceStateMetadataWarning:       0,
	}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = WorkItemEvidenceStateExactProviderFact
		}
		if _, ok := counts[state]; ok {
			counts[state]++
		}
	}
	missingCount := 0
	if len(rows) == 0 {
		missingCount = 1
	}
	return []attribute.KeyValue{
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceQueryCount, 1),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceResultCount, len(rows)),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceStaleCount, counts[WorkItemEvidenceStateStaleEvidence]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidencePermissionHiddenCount, counts[WorkItemEvidenceStatePermissionHidden]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount, counts[WorkItemEvidenceStateRejectedUnsafePayload]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount, counts[WorkItemEvidenceStateUnsupportedLinkType]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceMetadataWarningCount, counts[WorkItemEvidenceStateMetadataWarning]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceMissingCount, missingCount),
		attribute.Bool(telemetry.SpanAttrWorkItemEvidenceTruncated, truncated),
	}
}
