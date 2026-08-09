// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// Bounded reasons a shared-projection intent row could not be routed to a write
// statement. The distinction is load-bearing rather than cosmetic: one of these
// means the row can never become an edge, and the other can mean this binary is
// simply older than the producer that emitted the row.
const (
	// UnroutableReasonMissingRequiredField marks a row whose required MATCH
	// identifiers are absent from its payload. No writer version can route it;
	// the row is permanently unroutable and the loss is real.
	UnroutableReasonMissingRequiredField = "missing_required_field"

	// UnroutableReasonNoStatementForType marks a row whose relationship or
	// entity type has no write statement in THIS binary. That is not
	// necessarily a malformed row: during a rolling upgrade a newer producer
	// can emit a type an older writer does not know yet, and the same row
	// would route fine once the writer catches up.
	//
	// Separated from the missing-field case so an operator can tell a genuine
	// data loss from version skew. A spike in this reason during a deploy is a
	// rollout problem, not a corrupt-payload problem, and the remedy is
	// different: finish the upgrade and re-ingest, rather than fix a producer.
	UnroutableReasonNoStatementForType = "no_statement_for_type"
)

// SharedProjectionUnroutableRow identifies one intent row a canonical edge
// write could not route, with enough context to find it again without the
// payload: the intent id joins back to shared_projection_intents, and the
// scope/generation/domain triple is how an operator scopes a query.
type SharedProjectionUnroutableRow struct {
	IntentID         string
	ProjectionDomain string
	PartitionKey     string
	RepositoryID     string
	ScopeID          string
	GenerationID     string
	EvidenceSource   string
	// Reason is one of the bounded Unroutable* constants above.
	Reason string
	// DecidedAt is when the writer rejected the row.
	DecidedAt time.Time
}

// SharedProjectionWriteReport carries what a canonical edge write could not do,
// alongside what it did.
//
// It exists because "the write succeeded" and "every row became an edge" are
// different statements, and conflating them is the #5984 defect: a batch where
// some rows route and others do not returns no error, and the worker completes
// every intent in it — including the rows that produced nothing. Returning the
// rejected rows makes that loss available to the caller that owns completion,
// instead of leaving it in a log line.
type SharedProjectionWriteReport struct {
	// UnroutableRows are the rows that produced no edge. Empty on the ordinary
	// path, including for a batch made only of control rows that carry no edge
	// by design (see CarriesNoEdge) — those are not losses.
	UnroutableRows []SharedProjectionUnroutableRow
}

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
