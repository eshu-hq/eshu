// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// This file holds the evidence-state classification for the work_item evidence
// read surface: mapping one decoded fact to its truth label, validating a
// caller-supplied state, summarizing the states in a page, and shaping the
// per-state span counters. The evidence-state constants themselves live with
// the rest of the read-surface contract in evidence.go.

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
		return EvidenceStateMetadataWarning
	}
	if state := strings.TrimSpace(querycontract.StringVal(payload, "evidence_state")); knownWorkItemEvidenceState(state) {
		return state
	}
	if querycontract.BoolVal(payload, "permission_hidden") ||
		querycontract.StringVal(payload, "failure_class") == "permission_hidden" ||
		querycontract.StringVal(payload, "visibility_state") == "permission_hidden" {
		return EvidenceStatePermissionHidden
	}
	if querycontract.StringVal(payload, "source_freshness") == "stale" ||
		querycontract.StringVal(payload, "freshness_state") == "stale" {
		return EvidenceStateStaleEvidence
	}
	if fact.FactKind == "work_item.external_link" {
		state := strings.TrimSpace(querycontract.StringVal(payload, "provider_support_state"))
		switch {
		case strings.Contains(state, "unsupported"):
			return EvidenceStateUnsupportedLinkType
		case strings.Contains(state, "rejected"):
			return EvidenceStateRejectedUnsafePayload
		}
	}
	if querycontract.StringVal(payload, "warning_reason") == "rejected_unsafe_payload" {
		return EvidenceStateRejectedUnsafePayload
	}
	return EvidenceStateExactProviderFact
}

// knownWorkItemEvidenceState reports whether a state token is one of the
// bounded evidence states the read surface promotes.
func knownWorkItemEvidenceState(state string) bool {
	return slices.Contains([]string{
		EvidenceStateExactProviderFact,
		EvidenceStateUnsupportedLinkType,
		EvidenceStateMissingEvidence,
		EvidenceStateStaleEvidence,
		EvidenceStatePermissionHidden,
		EvidenceStateRejectedUnsafePayload,
		EvidenceStateMetadataWarning,
	}, state)
}

// summarizeWorkItemEvidenceStates returns the sorted distinct evidence states
// present in a page of rows, or the missing-evidence state for an empty page.
func summarizeWorkItemEvidenceStates(rows []EvidenceRow) []string {
	if len(rows) == 0 {
		return []string{EvidenceStateMissingEvidence}
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = EvidenceStateExactProviderFact
		}
		seen[state] = struct{}{}
	}
	return advisory.SetToSortedSlice(seen)
}

// workItemEvidenceSpanAttributes shapes the bounded per-state counters an
// operator reads on the evidence-list span. It counts the concern states
// (stale, permission-hidden, rejected-unsafe-payload, unsupported-link-type,
// metadata-warning) plus result/missing counts; exact_provider_fact is the
// baseline and is not broken out.
func workItemEvidenceSpanAttributes(rows []EvidenceRow, truncated bool) []attribute.KeyValue {
	counts := map[string]int{
		EvidenceStateStaleEvidence:         0,
		EvidenceStatePermissionHidden:      0,
		EvidenceStateRejectedUnsafePayload: 0,
		EvidenceStateUnsupportedLinkType:   0,
		EvidenceStateMetadataWarning:       0,
	}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = EvidenceStateExactProviderFact
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
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceStaleCount, counts[EvidenceStateStaleEvidence]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidencePermissionHiddenCount, counts[EvidenceStatePermissionHidden]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount, counts[EvidenceStateRejectedUnsafePayload]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount, counts[EvidenceStateUnsupportedLinkType]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceMetadataWarningCount, counts[EvidenceStateMetadataWarning]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceMissingCount, missingCount),
		attribute.Bool(telemetry.SpanAttrWorkItemEvidenceTruncated, truncated),
	}
}
