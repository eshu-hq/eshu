// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// UnroutableReasonMissingRequiredField is the root spelling of
// [sharedintent.UnroutableReasonMissingRequiredField].
const UnroutableReasonMissingRequiredField = sharedintent.UnroutableReasonMissingRequiredField

// UnroutableReasonNoStatementForType is the root spelling of
// [sharedintent.UnroutableReasonNoStatementForType].
const UnroutableReasonNoStatementForType = sharedintent.UnroutableReasonNoStatementForType

// SharedProjectionUnroutableRow is the root spelling of
// [sharedintent.UnroutableRow].
type SharedProjectionUnroutableRow = sharedintent.UnroutableRow

// SharedProjectionWriteReport is the root spelling of
// [sharedintent.WriteReport].
type SharedProjectionWriteReport = sharedintent.WriteReport

// SharedProjectionUnroutableWriter persists durable rows for intents that could
// not be routed to a write statement.
//
// Unlike QuarantinedFactWriter, a failure here MUST fail the owning cycle. The
// two are not analogous despite the similar shape: a quarantined fact is
// recorded alongside a work item that still exists and can be inspected, while
// an unroutable intent is about to be COMPLETED — after which nothing else
// records that it produced no edge, because completed intents are never
// reopened by the durable upsert. Making this write best-effort would restore
// the exact silent loss #5984 fixed, in a narrower window.
//
// Implementations MUST be idempotent: a cycle that crashes between this write
// and MarkIntentsCompleted re-runs the whole batch, so an ON CONFLICT DO
// NOTHING upsert keyed on the intent id is the expected shape.
type SharedProjectionUnroutableWriter interface {
	WriteUnroutableIntents(ctx context.Context, rows []SharedProjectionUnroutableRow) error
}
