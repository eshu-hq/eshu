// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"encoding/json"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Satellite-stamped NVD window advance verdict keys. The satellite stamps
// exactly these keys into the source snapshot payload beside
// coverage_descriptor (eshu-hq/eshu#9877 Path A); the values here are pinned
// against the satellite's copy by the verdict-shape tests on both sides.
// The commit path reads advance-to, head, and should-advance; the anomaly
// reason is informational (a hold rides on should-advance=false), so the key
// stays pinned here while production leaves it unread.
const (
	nvdWindowAdvanceToPayloadKey     = "nvd_window_advance_to_unix_us"
	nvdWindowHeadPayloadKey          = "nvd_window_head_unix_us"
	nvdWindowShouldAdvancePayloadKey = "nvd_window_should_advance"
	nvdWindowAnomalyPayloadKey       = "nvd_window_time_anomaly_reason"
)

// nvdWindowSnapshotFactKind identifies the source snapshot fact carrying the
// satellite-stamped coverage descriptor and window advance verdict.
const nvdWindowSnapshotFactKind = "vulnerability.source_snapshot"

// armNVDWindowCursorWriter returns the commit-transaction cursor writer for
// one collected window-scope generation, or nil when the cursor must hold.
// The writer runs inside the generation's own commit transaction, so the
// cursor advances with its window's facts or not at all; the CAS keeps the
// max when two claims planned from one cursor commit in either order. No
// verdict, ShouldAdvance=false, or a degenerate advance-to arms nothing:
// facts still commit, the cursor holds. It mirrors the host-owned
// side-write pattern: the extracted collector has no storage handle by
// design, so the host writes the row.
func (s *Source) armNVDWindowCursorWriter(
	item workflow.WorkItem,
	observedAt time.Time,
	envelopes []facts.Envelope,
) collector.GenerationStateWriter {
	if !vulnsource.IsNVDWindowScope(item.ScopeID) {
		return nil
	}
	payload, ok := nvdWindowVerdictPayload(envelopes)
	if !ok {
		return nil
	}
	shouldAdvance, ok := payloadBool(payload, nvdWindowShouldAdvancePayloadKey)
	if !ok || !shouldAdvance {
		return nil
	}
	advanceTo, ok := payloadMicros(payload, nvdWindowAdvanceToPayloadKey)
	if !ok || advanceTo <= 0 {
		return nil
	}
	head, _ := payloadMicros(payload, nvdWindowHeadPayloadKey)
	if observedAt.IsZero() {
		observedAt = s.clock().UTC()
	}
	return vulnerabilitystore.NVDWindowCursorWriter(
		s.collectorInstanceID,
		advanceTo,
		head,
		observedAt.UTC(),
	)
}

// nvdWindowVerdictPayload returns the satellite-stamped verdict payload of
// the first source snapshot fact in the generation, or false when no
// snapshot fact is present.
func nvdWindowVerdictPayload(envelopes []facts.Envelope) (map[string]any, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != nvdWindowSnapshotFactKind {
			continue
		}
		if len(envelope.Payload) == 0 {
			return nil, false
		}
		return envelope.Payload, true
	}
	return nil, false
}

// payloadBool reads a strict boolean verdict field. Anything but an actual
// true boolean holds the cursor.
func payloadBool(payload map[string]any, key string) (bool, bool) {
	value, ok := payload[key].(bool)
	return value, ok
}

// payloadMicros reads a Unix-microsecond verdict field across the JSON
// number shapes the claim transport produces: the satellite stamps int64,
// and a JSON round trip decodes it as float64.
func payloadMicros(payload map[string]any, key string) (int64, bool) {
	switch value := payload[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		return int64(value), true
	case json.Number:
		if micros, err := value.Int64(); err == nil {
			return micros, true
		}
	}
	return 0, false
}
