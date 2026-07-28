// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"
)

// reducerFactBatchInsertQuery is a writer-local bulk insert that sends N fact
// rows in a single round-trip using unnest. It is intentionally separate from
// canonicalReducerFactInsertQuery (the shared single-row statement) so the many
// per-row callers of that query are not disturbed; the column list, conflict
// key, and ON CONFLICT update set otherwise mirror it so a batched writer
// produces the same fact rows a per-row loop would.
//
// # Why fencing_token is written here (#5847)
//
// The column exists so that two passes colliding on one fact_id can be RANKED.
// It is written at BIRTH, as the last column ($16), rather than being left to
// the table's DEFAULT 0: a row resting at 0 makes the conflict guard below inert
// for its own domain, because `0 <= EXCLUDED.fencing_token` holds for every
// incoming pass, so the guard would admit stale content unconditionally while
// the domain looked fenced. Stamping on the insert is what gives the guard
// something to compare against.
//
// Only container_image_identity opts in today. It needs the ranking because it
// sits in the bootstrap correlation reopen slice
// (go/cmd/bootstrap-index/bootstrap_pipeline.go), so its intent is deliberately
// replayed once the cross-scope OCI generation activates — and the reducer queue
// does not order those replays for it. The queue's in-flight exclusion requires
// a LIVE lease (go/internal/storage/postgres/reducer_queue_batch_query.go) while
// the base predicate re-admits an item whose claim_until has already passed, and
// lease expiry IS the stalled-worker case, since heartbeat loss is quarantined
// only after Handle returns (reducer/service.go). So a worker that loaded
// evidence, stalled past its lease, and landed after the worker that overtook it
// is a reachable shape, and without the guard its poorer payload wins.
//
// The column is appended last rather than interleaved at its table position so
// the existing $1..$15 bind mapping — which IS the column contract this
// statement is reviewed on — is untouched by the addition.
//
// # Why the conflict clause is guarded rather than merged
//
// The conflict update is gated on `fact_records.fencing_token <=
// EXCLUDED.fencing_token`, so a stale pass's upsert is rejected WHOLE — content
// columns included — rather than being merged into the existing row.
//
// Raising only the token (`fencing_token = GREATEST(existing, excluded)`) with
// the content columns assigned unconditionally protects the token and nothing
// else, and that combination is worse than no fence at all. This domain's fact
// identity embeds only (scope_id, generation_id, image_ref, outcome), while
// source_revision, source_revision_provenance, build_provenance_repository_ids
// and evidence_fact_ids are payload-only and are filled in by cross-scope
// enrichment (applyCIRunDigestRevision/applySLSADigestRevision) whose visibility
// depends on which generations are active at load time. So two passes that agree
// on the outcome collide on the same fact_id with DIFFERENT payloads — a pass
// that read before the CI/SLSA generation activated carries a poorer one. Let the
// stalled pass overwrite the content while GREATEST keeps the fresher token, and
// the row advertises a freshness its payload does not have. Measured on Postgres
// 16 by TestReducerFactBatchInsertRejectsStaleContentUpsertLive, which is red on
// the GREATEST form.
//
// `<=`, not `<`. A retry, a redelivery, or a second chunk of the same pass
// carries the SAME watermark, because the watermark is the evidence-read time
// and a pass reads its evidence once; `<` would discard all of them while
// reporting success. With the guard in place an explicit GREATEST would be dead
// text — no path can lower a token, so the surviving value is always the larger.
//
// The guard is inert for the six callers that never opted in: they bind 0 and
// their rows sit at the column default 0, so `0 <= 0` holds and the update
// proceeds exactly as before. That is a property of the SQL rather than of the
// arguments, so it is proven against real Postgres by
// TestReducerFactBatchInsertStaysInertForUnfencedWritersLive rather than
// asserted.
//
// The guard clause itself is the same one upsertFactBatchSuffix
// (go/internal/storage/postgres/facts_streaming.go, #4444) uses to fence the
// collector ingest path into this same table. The PAIRING differs, and the
// difference is worth naming rather than glossing. There the guard is issued
// through upsertFactBatchReturningAccepted, whose RETURNING reads back the
// fact_ids the guard actually accepted, because that path derives downstream
// work from the write and a fenced-out row must not feed it (#4444, codex P1).
// Here the guard is taken WITHOUT that readback: nothing downstream is derived
// from which rows this insert accepted, and a fenced-out row is one the guard
// rejected precisely because a FRESHER row already occupies that fact_id.
//
// What the missing readback does cost is legibility. A pass fenced out in WHOLE
// still reports CanonicalWrites=N, which is byte-identical to a pass that landed
// normally. The rows are right either way; the summary cannot tell an operator
// which of the two happened. Adding the readback here would need its own live
// proof and its own concurrency argument, so it is stated rather than assumed
// away.
const reducerFactBatchInsertQuery = `
INSERT INTO fact_records (
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload,
    fencing_token
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload::jsonb,
    fencing_token
FROM unnest(
    $1::text[],
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[],
    $6::text[],
    $7::text[],
    $8::text[],
    $9::text[],
    $10::text[],
    $11::text[],
    $12::timestamptz[],
    $13::timestamptz[],
    $14::bool[],
    $15::text[],
    $16::bigint[]
) AS t(
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    collector_kind,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload,
    fencing_token
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind         = EXCLUDED.fact_kind,
    stable_fact_key   = EXCLUDED.stable_fact_key,
    collector_kind    = EXCLUDED.collector_kind,
    source_confidence = EXCLUDED.source_confidence,
    source_system     = EXCLUDED.source_system,
    source_fact_key   = EXCLUDED.source_fact_key,
    source_uri        = EXCLUDED.source_uri,
    source_record_id  = EXCLUDED.source_record_id,
    observed_at       = EXCLUDED.observed_at,
    ingested_at       = EXCLUDED.ingested_at,
    is_tombstone      = EXCLUDED.is_tombstone,
    payload           = EXCLUDED.payload,
    fencing_token     = EXCLUDED.fencing_token
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
`

// reducerFactBatchSize bounds how many fact rows are sent per unnest statement.
// Both inserts bind 16 columns per row — the unversioned one adds fencing_token,
// the versioned one schema_version — so 1000 rows consumes at most 16000 bind
// parameters, well under Postgres' 65535 parameter ceiling, while keeping each
// statement large enough to amortise round-trip cost. The bound also caps
// per-statement memory and lock footprint for a single scope.
const reducerFactBatchSize = 1000

// reducerFactRow is one canonical fact-record row for a batched insert. The
// field order and types mirror the positional arguments of
// canonicalReducerFactInsertQuery so a batched writer is a drop-in replacement
// for the per-row loop it replaces.
type reducerFactRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	CollectorKind    string
	SourceConfidence string
	SourceSystem     string
	SourceFactKey    string
	SourceURI        *string
	SourceRecordID   *string
	ObservedAt       time.Time
	IngestedAt       time.Time
	IsTombstone      bool
	Payload          string
	// FencingToken is the row's write-ordering watermark, carried on the INSERT
	// so the conflict guard has something to rank a colliding pass against.
	// Leave it zero unless the domain's intent can be replayed by two workers
	// holding different views of the evidence; see reducerFactBatchInsertQuery
	// for why 0 is not a safe resting state for one that can.
	FencingToken int64
}

// dedupeReducerFactRowsByFactID collapses rows sharing a fact_id to a single
// row carrying the LAST occurrence's value, reproducing the per-row loop's
// last-write-wins upsert semantics. A single `unnest` INSERT ... ON CONFLICT
// (fact_id) DO UPDATE statement rejects a batch that carries the same conflict
// key twice (Postgres: "ON CONFLICT DO UPDATE command cannot affect row a second
// time"), which would fail the writer and wedge the leased projection intent
// into an infinite retry (the #2809/#2855 failure class). The per-row loop this
// batching replaces issued one statement per row and upserted duplicates
// silently, last write winning; deduping to the last occurrence reproduces that
// exact final `fact_records` state without the poison-pill statement.
func dedupeReducerFactRowsByFactID[T any](rows []T, factID func(T) string) []T {
	if len(rows) < 2 {
		return rows
	}
	lastIdx := make(map[string]int, len(rows))
	for i, row := range rows {
		lastIdx[factID(row)] = i
	}
	if len(lastIdx) == len(rows) {
		return rows
	}
	out := make([]T, 0, len(lastIdx))
	for i, row := range rows {
		if lastIdx[factID(row)] == i {
			out = append(out, row)
		}
	}
	return out
}

// reducerBatchInsertFacts upserts rows in bounded chunks of reducerFactBatchSize
// using reducerFactBatchInsertQuery. It issues ceil(len(rows)/batchSize)
// ExecContext calls instead of one per row, so a scope with N rows costs
// O(N/batchSize) round-trips rather than O(N). Each chunk is a single statement;
// callers that need all chunks committed atomically must pass a transaction as
// db. Rows sharing a fact_id are deduped to their last occurrence
// (last-write-wins) to match the per-row loop and avoid the ON CONFLICT
// double-affect error. An empty rows slice issues no statements.
func reducerBatchInsertFacts(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactRow,
) error {
	rows = dedupeReducerFactRowsByFactID(rows, func(r reducerFactRow) string { return r.FactID })
	for start := 0; start < len(rows); start += reducerFactBatchSize {
		end := start + reducerFactBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := execReducerFactChunk(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// execReducerFactChunk sends one bounded chunk as a single unnest statement.
//
// This and execReducerFactVersionedChunk are deliberately kept as separate,
// explicit parallel functions rather than unified behind a shared helper. Each
// mirrors one distinct positional query. Both bind 16 columns and differ by
// WHICH sixteenth, not by count: the unversioned insert appends fencing_token at
// $16, the versioned insert interleaves schema_version at position 6 and shifts
// every later parameter by one. Same arity, different mapping, is precisely the
// shape a shared builder would silently get wrong — and the array order here IS
// the column↔bind-parameter contract this change was reviewed on. Collapsing
// them behind a generic argument builder would hide that mapping and reintroduce
// exactly the column-misplacement risk this change was reviewed against on the
// governed-fact path, for no runtime benefit; the duplication is intentional.
func execReducerFactChunk(
	ctx context.Context,
	db workloadIdentityExecer,
	chunk []reducerFactRow,
) error {
	n := len(chunk)
	factIDs := make([]string, n)
	scopeIDs := make([]string, n)
	generationIDs := make([]string, n)
	factKinds := make([]string, n)
	stableKeys := make([]string, n)
	collectorKinds := make([]string, n)
	sourceConfidences := make([]string, n)
	sourceSystems := make([]string, n)
	sourceFactKeys := make([]string, n)
	sourceURIs := make([]*string, n)
	sourceRecordIDs := make([]*string, n)
	observedAts := make([]time.Time, n)
	ingestedAts := make([]time.Time, n)
	isTombstones := make([]bool, n)
	payloads := make([]string, n)
	fencingTokens := make([]int64, n)

	for i, row := range chunk {
		factIDs[i] = row.FactID
		scopeIDs[i] = row.ScopeID
		generationIDs[i] = row.GenerationID
		factKinds[i] = row.FactKind
		stableKeys[i] = row.StableFactKey
		collectorKinds[i] = row.CollectorKind
		sourceConfidences[i] = row.SourceConfidence
		sourceSystems[i] = row.SourceSystem
		sourceFactKeys[i] = row.SourceFactKey
		sourceURIs[i] = row.SourceURI
		sourceRecordIDs[i] = row.SourceRecordID
		observedAts[i] = row.ObservedAt
		ingestedAts[i] = row.IngestedAt
		isTombstones[i] = row.IsTombstone
		payloads[i] = row.Payload
		fencingTokens[i] = row.FencingToken
	}

	if _, err := db.ExecContext(
		ctx,
		reducerFactBatchInsertQuery,
		factIDs,
		scopeIDs,
		generationIDs,
		factKinds,
		stableKeys,
		collectorKinds,
		sourceConfidences,
		sourceSystems,
		sourceFactKeys,
		sourceURIs,
		sourceRecordIDs,
		observedAts,
		ingestedAts,
		isTombstones,
		payloads,
		fencingTokens,
	); err != nil {
		return fmt.Errorf("batch insert reducer facts: %w", err)
	}
	return nil
}
