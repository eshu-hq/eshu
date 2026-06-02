# NornicDB Shadow-Read Comparison Gate

Status: implementation contract for issue #1287 and parent issue #431.
This PR adds a pure validation package and design guidance only; it does not
change production storage ownership, API/MCP routes, Postgres schema, NornicDB
schema, reducer behavior, or runtime defaults.

Owners: storage, query, reducer, graph backend, and reliability maintainers.

## 1. Purpose

The #1287 gate defines how Eshu will prove selected Postgres-backed content and
read-model answers can be reproduced by a NornicDB shadow read path before any
production cutover is proposed.

The gate is intentionally narrow. It compares current Postgres read-model
outputs with NornicDB shadow outputs for scoped, bounded reads. A passing
comparison means the shadow answer matched the production baseline for that
read model and scope. It does not mean NornicDB owns the data, that API/MCP
routes should switch, or that graph/search proof from issue #430 is sufficient
for durable store migration.

## 2. Covered Read Models

The first contract covers these read-model families:

| Read model | Current baseline | Shadow candidate | Required scope |
| --- | --- | --- | --- |
| Repository files | Postgres `ContentStore` file reads | NornicDB shadow content/read-model reader | repository or file |
| Content entities | Postgres `ContentStore` entity reads | NornicDB shadow content/read-model reader | repository or entity |
| Structural inventory | Query structural inventory read models | NornicDB shadow inventory reader | repository |
| Search documents | Curated `EshuSearchDocument` projection | NornicDB shadow search-document reader | repository or document |
| Repository context | Repository story/context read models | NornicDB shadow repository context reader | repository |
| Relationship evidence | Relationship evidence drilldown | NornicDB shadow relationship evidence reader | relationship or entity |

Adapters that gather these rows are future work. This PR only defines the
evidence record and validation behavior in `go/internal/storageeval`.

## 3. Evidence Contract

Every comparison record must include:

- `read_model`: the covered read-model family.
- `capability`: the capability or proof lane being evaluated.
- `scope`: the smallest bounded comparison scope.
- `limit`: the page or candidate bound used by both reads.
- `baseline`: the Postgres read result summary.
- `shadow`: the NornicDB read result summary.
- `verdict`: `match` for passing evidence.
- `fallback_behavior`: what production does if shadow proof fails.
- `failure_class`: `none` for passing evidence.

Each result summary records:

- backend label;
- stable digest of the canonicalized read-model output;
- truth label with level and basis;
- freshness state and observation time;
- latency;
- truncation;
- support status.

The digest is a proof handle, not a user-facing payload. Future proof runners
must canonicalize output deterministically before digesting so map ordering,
page ordering, and equivalent empty values do not create false drift.

## 4. Passing Gate

`ValidateShadowReadComparison` accepts a record only when all of these are
true:

- the read model is in the covered set;
- capability, supported scope, and positive limit are present;
- baseline backend is `postgres_read_model`;
- shadow backend is `nornicdb_shadow_read_model`;
- both results are fresh, supported, and non-truncated;
- both results have non-empty digests;
- truth labels match exactly;
- truth level is `exact` or `derived`, never `fallback`;
- digests match;
- verdict is `match`;
- fallback behavior is explicit;
- failure class is `none`.

This prevents a shadow path from silently downgrading to fallback truth,
claiming partial equality from truncated data, or promoting derived search/read
model output into canonical graph truth.

## 5. Rejected States

The gate rejects and classifies these states:

| State | Why it blocks parity |
| --- | --- |
| Missing shadow result | NornicDB did not reproduce the Postgres answer. |
| Stale shadow result | Equality against old shadow data is not production evidence. |
| Divergent digest | The two canonicalized outputs disagree. |
| Truth mismatch | The shadow path changed authority or basis. |
| Fallback truth | A fallback result cannot prove storage parity. |
| Truncation | Partial output can hide missing or extra rows. |
| Unsupported capability | The shadow backend cannot answer the scoped read. |
| Missing scope or limit | Unbounded proof is not acceptable. |
| Unsupported scope kind | The proof target is outside the bounded comparison set. |
| Missing fallback behavior | Operators cannot tell how production remains safe. |
| Missing failure class | The evidence record is incomplete for operators. |

Future proof runners may store rejected rows for diagnostics, but rejected rows
must not count as passing parity evidence.

## 6. Runtime Behavior

Production behavior remains unchanged while this gate is in use:

```text
API/MCP/CLI request
  -> existing Postgres or graph-backed production read
  -> optional proof runner performs shadow read out-of-band
  -> storageeval validates comparison evidence
  -> parity evidence informs a later cutover ADR
```

The shadow reader must not sit on the production request path until a later PR
proves latency, reliability, and rollback behavior. If shadow proof fails,
production fallback is to keep the Postgres-backed answer, fail closed for a
promoted internal-only proof, or return `unsupported_capability` for a future
capability that cannot be answered correctly.

## 7. Observability Requirements

Future proof runners must expose:

- comparison count by read model;
- comparison duration;
- parity drift count;
- latest drift time;
- failure class counts;
- fallback count by reason;
- baseline and shadow latency distribution;
- compared row, document, or entity count.

High-cardinality ids such as repository ids, file paths, entity ids,
relationship ids, document ids, graph handles, digests, and request ids belong
in logs or traces, not metric labels.

No-Observability-Change: this PR defines the labels and validation behavior but
does not alter hosted runtime telemetry.

## 8. Non-Goals

This PR does not:

- add a NornicDB content/read-model adapter;
- write shadow rows;
- change production API/MCP/CLI routes;
- change Postgres or NornicDB schemas;
- remove or bypass `ContentStore`;
- alter reducer truth or graph projection;
- persist comparison reports;
- close parent issue #431.

## 9. Evidence For This PR

No-Regression Evidence: `go test ./internal/storageeval -count=1` proves the
gate accepts matching bounded evidence and rejects unsupported read models,
missing scope, unsupported scope kind, unbounded comparison, missing truth
labels, missing shadow results, stale shadow results, divergent digests,
fallback truth, truth mismatch, truncated results, unsupported capability,
missing fallback behavior, non-match verdicts, missing failure class, and
negative latency.

No-Observability-Change: the package is pure and emits no hosted metrics,
spans, or logs. Future proof runners must emit the signals listed above.

Source check date: 2026-06-02.

Sources used:

- `go/internal/query/contract.go`
- `go/internal/query/read-models.md`
- `go/internal/searchdocs/project.go`
- `go/internal/storage/postgres/content_store.go`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/search-document-projection.md`
- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
