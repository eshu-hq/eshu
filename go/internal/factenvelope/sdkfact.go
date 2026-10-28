// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factenvelope

import (
	"fmt"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SDKFactFromInternal projects one durable internal envelope onto its
// collector SDK wire shape, the inverse of InternalFromSDKFact. Host-owned
// fields that never cross the wire (FactID, CollectorKind, FencingToken)
// are dropped by construction, and the durable envelope carries no
// redactions, so the projection carries none. The returned fact shares the
// envelope's payload map: treat it as read-only.
//
// Analyzer byte accounting uses this projection to measure the exact JSON
// wire bytes each envelope will cost inside a paged SDK result, before any
// satellite port exists to do the mapping itself.
func SDKFactFromInternal(envelope facts.Envelope) (sdkcollector.Fact, error) {
	confidence, err := sdkConfidence(envelope.SourceConfidence)
	if err != nil {
		return sdkcollector.Fact{}, err
	}
	return sdkcollector.Fact{
		Kind:             envelope.FactKind,
		SchemaVersion:    envelope.SchemaVersion,
		StableKey:        envelope.StableFactKey,
		SourceConfidence: confidence,
		ObservedAt:       envelope.ObservedAt.UTC(),
		Tombstone:        envelope.IsTombstone,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: envelope.SourceRef.SourceSystem,
			ScopeID:      envelope.SourceRef.ScopeID,
			GenerationID: envelope.SourceRef.GenerationID,
			FactKey:      envelope.SourceRef.FactKey,
			URI:          envelope.SourceRef.SourceURI,
			RecordID:     envelope.SourceRef.SourceRecordID,
		},
		Payload: envelope.Payload,
	}, nil
}

// sdkConfidence maps a durable source confidence onto the wire value. Anything
// the wire cannot represent fails closed: emitting it would only die later in
// SDK validation after the bytes were already accounted.
func sdkConfidence(value string) (sdkcollector.SourceConfidence, error) {
	switch value {
	case facts.SourceConfidenceObserved:
		return sdkcollector.SourceConfidenceObserved, nil
	case facts.SourceConfidenceReported:
		return sdkcollector.SourceConfidenceReported, nil
	case facts.SourceConfidenceInferred:
		return sdkcollector.SourceConfidenceInferred, nil
	case facts.SourceConfidenceDerived:
		return sdkcollector.SourceConfidenceDerived, nil
	default:
		return "", fmt.Errorf("source_confidence %q has no SDK wire value", value)
	}
}
