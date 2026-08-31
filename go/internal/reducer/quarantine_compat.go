// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file is the transitional compatibility surface for the fact-decode and
// quarantine mechanism that moved to [factdecode] (issue #6061). Reducer-root
// call sites and the external packages that name the exported quarantine types
// keep their current spelling; each entry is deleted once its last caller has
// moved into a family subpackage.

// QuarantinedFactRecord is the durable dead-letter row for one malformed fact.
type QuarantinedFactRecord = factdecode.QuarantinedFactRecord

// QuarantinedFactWriter persists quarantined facts, implemented by the Postgres
// input-invalid fact store.
type QuarantinedFactWriter = factdecode.QuarantinedFactWriter

// quarantinedFact is the in-flight quarantine value produced by a decode
// failure, before it is persisted as a QuarantinedFactRecord.
type quarantinedFact = factdecode.QuarantinedFact

// factDecodeError classifies a malformed payload as a terminal dead letter.
type factDecodeError = factdecode.FactDecodeError

// WithQuarantineWriter forwards to [factdecode.WithQuarantineWriter].
func WithQuarantineWriter(ctx context.Context, writer QuarantinedFactWriter) context.Context {
	return factdecode.WithQuarantineWriter(ctx, writer)
}

// newFactDecodeError forwards to [factdecode.NewFactDecodeError].
func newFactDecodeError(factKind string, err error) *factDecodeError {
	return factdecode.NewFactDecodeError(factKind, err)
}

// partitionDecodeFailures forwards to [factdecode.PartitionDecodeFailures].
func partitionDecodeFailures(env facts.Envelope, err error) (quarantinedFact, bool, error) {
	return factdecode.PartitionDecodeFailures(env, err)
}

// quarantinedAttributeShapeFact forwards to
// [factdecode.QuarantinedAttributeShapeFact].
func quarantinedAttributeShapeFact(env facts.Envelope, err error) quarantinedFact {
	return factdecode.QuarantinedAttributeShapeFact(env, err)
}

// attributeShapeAsFactDecodeError forwards to
// [factdecode.AttributeShapeAsFactDecodeError].
func attributeShapeAsFactDecodeError(factKind string, err error) error {
	return factdecode.AttributeShapeAsFactDecodeError(factKind, err)
}

// inputInvalidSubSignals forwards to [factdecode.InputInvalidSubSignals].
func inputInvalidSubSignals(count int) map[string]float64 {
	return factdecode.InputInvalidSubSignals(count)
}

// recordQuarantinedFacts forwards to [factdecode.RecordQuarantinedFacts].
func recordQuarantinedFacts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain Domain,
	scopeID, generationID string,
	quarantined []quarantinedFact,
) int {
	return factdecode.RecordQuarantinedFacts(ctx, instruments, domain, scopeID, generationID, quarantined)
}
