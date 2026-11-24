// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// M6 narrow-store write shapes (#11142). The keyed tick's floor is the wide
// fact_records heap: per touched identifier it fetches every candidate copy
// as a wide row and discards most after the fetch. The narrow store keeps one
// slim row per live finding so the tick's first pass never touches the wide
// heap. Three physical layouts stand side by side in this proof:
//
//   - prototype: one narrow table carrying the key, the ranking inputs and
//     the winners-only payload, plus an all-column covering index and the
//     canonical key. The read path never visits fact_records.
//   - deduped: the same table without the derivable and duplicated bytes
//     (payload, source_confidence and the redundant wide columns). The first
//     pass ranks from the narrow row; the winners-only payload is fetched
//     from fact_records by fact_id after the ranking.
//   - index-expression: no second table at all. The ranking inputs ride as
//     trailing key expressions of the keyed winners index family, so the
//     first pass is an index-only probe of fact_records. The write cost is
//     index maintenance on the fact write, not a second heap write.
//
// The populate statements derive their candidate set from the shipped
// full-path inner select (supplyChainImpactWinnerActiveFactsInnerSQL), so a
// shape cannot drift from the live set: the same liveness join feeds the
// shipped winners and every narrow table. The differential in the M6 proof
// runs the shipped winner body over each shape's candidate set and requires
// byte-identical winner digests.

// NarrowWriteShape names one M6 narrow-store physical layout.
type NarrowWriteShape string

const (
	// NarrowShapePrototype is shape (a): table plus all-column covering index
	// plus canonical key.
	NarrowShapePrototype NarrowWriteShape = "prototype"
	// NarrowShapeDeduped is shape (b): no derivable or duplicated bytes.
	NarrowShapeDeduped NarrowWriteShape = "deduped"
	// NarrowShapeIndexExpression is shape (c): ranking inputs as trailing key
	// expressions of the keyed index, no second table.
	NarrowShapeIndexExpression NarrowWriteShape = "index-expression"
)

// NarrowWriteShapes returns the three M6 layouts in ruling order.
func NarrowWriteShapes() []NarrowWriteShape {
	return []NarrowWriteShape{NarrowShapePrototype, NarrowShapeDeduped, NarrowShapeIndexExpression}
}

// NarrowShapeTableName names the shape's narrow table, or "" for the
// index-expression shape, which keeps no second table.
func NarrowShapeTableName(shape NarrowWriteShape) string {
	switch shape {
	case NarrowShapePrototype:
		return "sci_winners_narrow_prototype"
	case NarrowShapeDeduped:
		return "sci_winners_narrow_deduped"
	default:
		return ""
	}
}

// narrowShapeKeyColumns lists the five-tuple probe columns every narrow row
// carries, in 291 index order.
const narrowShapeKeyColumns = `fact_id, scope_id, generation_id,
    package_id, repository_id, subject_digest, c0, advisory_id`

// narrowShapeRankColumns lists the ranking inputs every narrow row carries:
// the partition key plus the three ORDER BY scalars of the winner select.
const narrowShapeRankColumns = `canonical_key, priority_score,
    has_payload_finding_id, suppression_state`

// narrowShapeKeyExprs renders the five-tuple probe expressions over a
// fact_records alias, byte-identical to the 291 probe in
// buildWinnersKeyedActiveFactsSQL so the planner can serve the shape probes
// from the same index family.
func narrowShapeKeyExprs(alias string) string {
	payload := alias + ".payload"
	return `COALESCE(` + payload + `->>'package_id', ''),
    COALESCE(` + payload + `->>'repository_id', ''),
    COALESCE(` + payload + `->>'subject_digest', ''),
    COALESCE(NULLIF(` + payload + `->>'cve_id', ''), NULLIF(` + payload + `->>'advisory_id', ''), ''),
    COALESCE(` + payload + `->>'advisory_id', '')`
}

// narrowShapeRankExprs renders the ranking inputs over a payload expression,
// using the same shipped helpers as the winner select body so the narrow row
// cannot rank differently from the shipped select.
func narrowShapeRankExprs(payload string) string {
	return supplyChainImpactCanonicalKeyConcat(payload, "        ", "    ") + `,
    ` + supplyChainImpactPriorityScoreExpr(payload) + `,
    ` + supplyChainImpactHasPayloadFindingIDExpr(payload) + `,
    ` + supplyChainImpactSuppressionStateExpr(payload)
}

// NarrowShapeDDL creates one shape's structure: the narrow table plus its
// index for the table shapes, the trailing-expression index for the
// index-expression shape. IF NOT EXISTS keeps a re-run idempotent.
//
// Shape (c) carries the three ORDER BY scalars as trailing key expressions
// (INCLUDE cannot hold expressions). canonical_key stays out: its eleven
// concatenated payload fields can exceed the index row-size limit, so the
// first pass resolves it per candidate while priority, the finding-id flag
// and suppression state come straight from the index.
//
// Measured boundary (M6 proof, PostgreSQL 18): the (c) probe is an Index Scan,
// not an Index Only Scan. Index-only eligibility dies the moment the query
// references a base-column Var inside an expression (planner check on the
// stored columns), which every payload-expression probe does — an isolated
// fixture (plain and expression indexes, selective predicate, fully set
// visibility map) reproduces it: plain covering index goes Index Only Scan,
// any expression index stays Index Scan. The residual win is narrower index
// tuples plus no TOAST detoast of the payload for the ranking scalars
// (measured: 4x fewer buffers than the shipped probe), at the measured
// index-maintenance write cost. The "index-only first pass" design note is
// therefore corrected, not assumed.
func NarrowShapeDDL(shape NarrowWriteShape) string {
	switch shape {
	case NarrowShapePrototype:
		return `CREATE TABLE IF NOT EXISTS sci_winners_narrow_prototype (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    subject_digest TEXT NOT NULL,
    c0 TEXT NOT NULL,
    advisory_id TEXT NOT NULL,
    canonical_key TEXT NOT NULL,
    priority_score INTEGER NOT NULL,
    has_payload_finding_id INTEGER NOT NULL,
    suppression_state TEXT NOT NULL,
    payload JSONB NOT NULL,
    source_confidence TEXT NOT NULL DEFAULT 'unknown'
);
CREATE INDEX IF NOT EXISTS sci_winners_narrow_prototype_covering_idx
    ON sci_winners_narrow_prototype
    (package_id, repository_id, subject_digest, c0, advisory_id)
    INCLUDE (fact_id, scope_id, generation_id, canonical_key, priority_score,
        has_payload_finding_id, suppression_state, payload, source_confidence);`
	case NarrowShapeDeduped:
		return `CREATE TABLE IF NOT EXISTS sci_winners_narrow_deduped (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    subject_digest TEXT NOT NULL,
    c0 TEXT NOT NULL,
    advisory_id TEXT NOT NULL,
    canonical_key TEXT NOT NULL,
    priority_score INTEGER NOT NULL,
    has_payload_finding_id INTEGER NOT NULL,
    suppression_state TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sci_winners_narrow_deduped_covering_idx
    ON sci_winners_narrow_deduped
    (package_id, repository_id, subject_digest, c0, advisory_id)
    INCLUDE (fact_id, scope_id, generation_id, canonical_key, priority_score,
        has_payload_finding_id, suppression_state);`
	case NarrowShapeIndexExpression:
		return `CREATE INDEX IF NOT EXISTS fact_records_sci_winners_rank_expr_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        (COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),
        (COALESCE(payload->>'advisory_id', '')),
        (` + supplyChainImpactPriorityScoreExpr("payload") + `),
        (` + supplyChainImpactHasPayloadFindingIDExpr("payload") + `),
        (` + supplyChainImpactSuppressionStateExpr("payload") + `),
        fact_id ASC
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;`
	default:
		return ""
	}
}

// NarrowShapePopulateSQL loads one shape's table from the shipped live set.
// The inner select is the shipped full-path candidate set, so the narrow
// table holds exactly the rows the shipped winners rank over: no more (which
// would waste WAL) and no fewer (which would change winners). The
// index-expression shape keeps no table and reports "".
func NarrowShapePopulateSQL(shape NarrowWriteShape) string {
	table := NarrowShapeTableName(shape)
	if table == "" {
		return ""
	}
	cols := narrowShapeKeyColumns + `,
    ` + narrowShapeRankColumns
	if shape == NarrowShapePrototype {
		cols += `,
    payload, source_confidence`
	}
	vals := `fact.fact_id, fact.scope_id, fact.generation_id,
    ` + narrowShapeKeyExprs("fact") + `,
    ` + narrowShapeRankExprs("fact.payload")
	if shape == NarrowShapePrototype {
		vals += `,
    fact.payload, fact.source_confidence`
	}
	return `INSERT INTO ` + table + ` (` + cols + `)
SELECT ` + vals + `
FROM (` + supplyChainImpactWinnerActiveFactsInnerSQL + `) AS fact
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    package_id = EXCLUDED.package_id,
    repository_id = EXCLUDED.repository_id,
    subject_digest = EXCLUDED.subject_digest,
    c0 = EXCLUDED.c0,
    advisory_id = EXCLUDED.advisory_id,
    canonical_key = EXCLUDED.canonical_key,
    priority_score = EXCLUDED.priority_score,
    has_payload_finding_id = EXCLUDED.has_payload_finding_id,
    suppression_state = EXCLUDED.suppression_state` +
		narrowShapePopulateWideUpdate(shape) + `;`
}

// narrowShapePopulateWideUpdate extends the upsert arm with the wide columns
// only the prototype carries.
func narrowShapePopulateWideUpdate(shape NarrowWriteShape) string {
	if shape == NarrowShapePrototype {
		return `,
    payload = EXCLUDED.payload,
    source_confidence = EXCLUDED.source_confidence`
	}
	return ""
}

// NarrowShapeFirstPassSQL renders one shape's candidate first pass over an
// identifier table with (package_id, repository_id, subject_digest, c0,
// advisory_id) columns: the tick's per-identifier probe, which is the
// statement the M6 read proof EXPLAINs. A full-set scan would misrepresent
// the access path (the planner seq-scans any full fetch); the tick probes
// one identifier at a time, so the proof does too. The table shapes join
// their narrow table to the identifiers; the index-expression shape runs the
// shipped narrow lateral over fact_records through its trailing-expression
// index. That probe is an Index Scan (see the DDL note: expression probes
// cannot go index-only on this stack); the ranking scalars still come from
// the index tuple with no payload TOAST detoast.
func NarrowShapeFirstPassSQL(shape NarrowWriteShape, identsTable string) string {
	switch shape {
	case NarrowShapePrototype:
		return `SELECT narrow.fact_id, narrow.scope_id, narrow.generation_id,
    narrow.package_id, narrow.repository_id, narrow.subject_digest, narrow.c0,
    narrow.advisory_id, narrow.canonical_key, narrow.priority_score,
    narrow.has_payload_finding_id, narrow.suppression_state, narrow.payload,
    narrow.source_confidence
FROM sci_winners_narrow_prototype AS narrow
JOIN ` + identsTable + ` AS ident
  ON ident.package_id = narrow.package_id
 AND ident.repository_id = narrow.repository_id
 AND ident.subject_digest = narrow.subject_digest
 AND ident.c0 = narrow.c0
 AND ident.advisory_id = narrow.advisory_id`
	case NarrowShapeDeduped:
		return `SELECT narrow.fact_id, narrow.scope_id, narrow.generation_id,
    narrow.package_id, narrow.repository_id, narrow.subject_digest, narrow.c0,
    narrow.advisory_id, narrow.canonical_key, narrow.priority_score,
    narrow.has_payload_finding_id, narrow.suppression_state
FROM sci_winners_narrow_deduped AS narrow
JOIN ` + identsTable + ` AS ident
  ON ident.package_id = narrow.package_id
 AND ident.repository_id = narrow.repository_id
 AND ident.subject_digest = narrow.subject_digest
 AND ident.c0 = narrow.c0
 AND ident.advisory_id = narrow.advisory_id`
	case NarrowShapeIndexExpression:
		return `SELECT ident.package_id, ident.repository_id, ident.subject_digest,
    ident.c0, ident.advisory_id, fact.fact_id, fact.priority_score,
    fact.has_payload_finding_id, fact.suppression_state
FROM ` + identsTable + ` AS ident
CROSS JOIN LATERAL (
    SELECT f.fact_id,
        ` + supplyChainImpactPriorityScoreExpr("f.payload") + ` AS priority_score,
        ` + supplyChainImpactHasPayloadFindingIDExpr("f.payload") + ` AS has_payload_finding_id,
        ` + supplyChainImpactSuppressionStateExpr("f.payload") + ` AS suppression_state
    FROM fact_records AS f
    WHERE (f.payload->>'package_id') = ident.package_id
      AND (f.payload->>'repository_id') = ident.repository_id
      AND (f.payload->>'subject_digest') = ident.subject_digest
      AND COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = ident.c0
      AND COALESCE(f.payload->>'advisory_id', '') = ident.advisory_id
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
    OFFSET 0
) AS fact`
	default:
		return ""
	}
}

// NarrowWinnersSelectSQL composes the shipped winner select body over an
// arbitrary active-facts table: the differential vehicle of the M6 proof.
// The body is the shipped one, so a digest match proves the shape preserves
// winner truth. It is the twin of winnersKeyedSelectSQL, which fixes the
// table to the keyed tick's temp table.
func NarrowWinnersSelectSQL(activeFactsTable string) string {
	return `WITH active_facts AS (SELECT * FROM ` + activeFactsTable + `)` +
		supplyChainImpactWinnerBodySQL
}
