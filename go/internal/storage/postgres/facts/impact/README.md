# storage/postgres/facts/impact

The supply-chain impact active-evidence branch catalog (#8880).

`FactStore.ListActiveSupplyChainImpactFacts` used to run one statement that
OR-ed about thirty identity arms over 21 fact kinds and paged it 500 rows at a
time. Postgres could not use an index for that disjunction, so every page
walked every active generation's facts and detoasted every `file` payload.
On the hosted corpus one npm page read 13.9 million buffers.

This package generates the replacement from one table:

| Piece | What it is |
| --- | --- |
| `Kinds()` | The 21 scanned kinds, each with a class and the census row that justified it. |
| `Branches()` | One id-resolution statement per (kind group, key) or per bounded kind, with its anchor index, bind order, and the inputs that gate it. |
| `NewIndexes()` | The 14 anchors migrations 212 to 225 create, one `CREATE INDEX CONCURRENTLY` per file. |
| `GuardPredicate()` | The foreign-key guard: growth and file rows that carry a key their branch does not probe. |
| `ScopeResolutionSQL`, `PayloadPageSQL` | The repository-to-scope resolution and the primary-key payload page. |

## How one load runs

1. Resolve `RepositoryIDs` and `FileRepositoryIDs` to active `(scope_id,
   active_generation_id)` pairs (`ScopeResolutionSQL`).
2. Run every branch whose gating lists are non-empty. Each statement reads
   `fact_records` through its anchor only (a MATERIALIZED `candidate` CTE),
   applies the pre-#8880 active-generation join once per distinct pair (a
   MATERIALIZED `active` CTE), and returns `(fact_id, fact_kind)`.
3. Dedupe the IDs, sort core and suppression IDs by byte order, keep the first
   `cap` suppressions and report truncation past it.
4. Fetch payloads in 500-ID primary-key pages, then page the canonical
   identity stream with its own cursor. All of it runs in one read-only
   repeatable-read snapshot on the primary.

## Classes

- Growth (vulnerability catalog, registry, SBOM, OCI, CI/CD, consumption,
  provider alerts): one key probe per contract-carried key. A key the
  contract never carries is a guard cell.
- Bounded (suppression, platform materialization, workload identity,
  service catalog): one statement per kind evaluates every applicable arm
  over a kind-partial anchor.
- File: the JS/TS `repo_id` probe and the resolved-scope probe; every other
  file cell is a guard cell (`codegraph/v1.File` carries none of them).

A kind changes class only with a new census. The census and the per-index
justification live in `docs/internal/evidence/8880-supply-chain-active-evidence.md`.

## Exactness

The scope-row arms of the old predicate (`scope.source_key`,
`scope.payload->>'repo_id'`, `scope.payload->>'id'`) render as
`fact.scope_id = ANY(resolved scopes)`: for a row the active-generation join
admits, `scope` is the row's own scope, and the resolution reads the same
snapshot. Every other arm is the old text.

## Telemetry

Branch names are the closed `branch` label of
`eshu_dp_supply_chain_impact_active_evidence_branch_seconds` and
`eshu_dp_supply_chain_impact_active_evidence_branch_rows_total`, with
`scope_resolution`, `payload_page`, and `identity`. Guard rows also count on
`eshu_dp_supply_chain_impact_active_evidence_foreign_key_guard_rows_total`
and log a WARN line naming the drifted kinds.
