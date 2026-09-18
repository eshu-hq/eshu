# #6753 — FIPS-safe Postgres digests

## Failure and affected flow

Ops-qa runs PostgreSQL 18.3 with OpenSSL FIPS enabled. A read-only
`SELECT md5('foo')` returned `could not compute MD5 hash: unsupported` (exit 1).
The old changed-since payload aggregation failed with the same error on a scope
with 158 active facts. This was a production query, not a migration failure.

The admin identity API derives `mapping_ref` from the provider, tenant,
workspace, role, and stored external group hash. List, create, and delete must
use the same expression. The group hash used by login does not change. The
reference is an opaque row handle, not a persisted column. The changed-since
counts and bounded sample reads use a payload digest only to compare prior and
current generation rows; their digest is neither persisted nor returned.

PostgreSQL 18 documents `sha256(bytea)` as a built-in binary-string function and
`encode(bytea, 'hex')` for wire-safe hexadecimal text. This change uses
`convert_to(text, 'UTF8')` to make the input bytes explicit. See the
[PostgreSQL binary-string functions](https://www.postgresql.org/docs/18/functions-binarystring.html).

## Compatibility and edge cases

The same composite-key input now produces a 64-hex SHA-256 mapping ref. Saved
32-hex MD5 refs cannot address a row on FIPS Postgres. The HTTP delete route
recognizes those refs, returns 409 with a list-and-retry instruction, and records
a denied governance audit reason. It does not silently report an absent mapping.
List the mappings again to obtain current refs; the underlying mapping rows
and OIDC group resolution are unchanged. Ops-qa had zero rows in
`identity_provider_group_role_mappings` at the time of this read-only check.
Other deployments may have rows, so the client transition is documented in the
HTTP API and OpenAPI. Unknown non-legacy refs retain the existing idempotent
no-op behavior. Tenant/workspace predicates and terminal-state guards on delete
are unchanged.

Changed-since keeps the same grouping, `MIN` aggregation shape,
classification order, and sample limit. Equal JSONB text still produces an
equal digest; tombstones are not hashed. For a stable key with multiple
different payloads, a different hash algorithm can choose a different minimum
digest and change its classification. The existing distinction between
`retired` and `superseded` remains. The internal digest stays as `bytea` to
avoid per-row hexadecimal encoding.

## Isolated ops-qa proof

The changed-since and initial mapping-list checks used read-only transactions
or `SELECT`. A later roundtrip used a transaction-scoped temporary table with
`search_path = pg_temp` and a five-second statement timeout. It rolled back and
confirmed the temporary table was removed. No application rows were written.

- `md5('foo')` and the old `MIN(md5(payload::text))` failed with the FIPS error.
  `encode(sha256(convert_to('foo', 'UTF8')), 'hex')` returned a 64-hex value.
- The exact revised `changedSinceCountsQuery`, extracted from the source, ran
  against the same 158-row generation as both prior and current. It returned
  8 unchanged content entities, 147 unchanged facts, and 3 unchanged files.
  The exact revised samples query returned two ordered fact handles.
- The exact revised mapping-list query, given one synthetic row through a CTE,
  returned one 64-hex ref. The live mapping table itself had zero rows.
- The exact revised create/list/delete queries, extracted from source, ran on
  the FIPS server against the temporary table. First create returned
  `inserted=true`; retry returned `inserted=false` and the same 64-hex ref.
  Wrong-tenant list and delete returned zero rows. Correct-tenant delete
  tombstoned one row; repeat delete returned zero. After `ROLLBACK`, the
  temporary table was absent. The roundtrip exited 0.
- In the measured 18,238-row ops-qa generation, no `(fact_category,
  stable_fact_key)` pair had multiple active rows. This does not establish that
  the full corpus has no duplicate keys.
- On a loaded 158-row scope, an initial encoded SHA-256 aggregate had 4.668 ms
  execution. On a loaded 18,238-row scope, a raw `bytea` SHA-256 aggregate had
  3,899.055 ms execution, versus 4,001.230 ms with hex encoding. The index
  scan was 368.594 ms and 110.702 ms respectively; most time was payload
  conversion and hashing under load. An encoded SHA-256 aggregate on a
  158,525-row scope reached a five-second statement timeout. These absolute
  times do not establish an endpoint latency bound under reducer drain.

## Controlled relative performance proof

A local non-FIPS PostgreSQL 18.6 instance ran both functions over the same
18,238 synthetic JSONB payloads, with one long payload. The synthetic table
averaged 1,064 stored bytes per payload; the measured ops-qa scope averaged
1,068 bytes (median 814, p95 1,794, p99 3,984, maximum 436,815). In two
alternating `EXPLAIN (ANALYZE, BUFFERS)` reads, `MIN(md5(payload::text))`
took 33.650 and 31.724 ms; `MIN(sha256(convert_to(payload::text, 'UTF8')))`
took 16.752 and 16.834 ms. The local comparison uses one server, table, and
query shape, so it supports no hash-function regression on this fixture. It
cannot be compared as an end-to-end speedup against loaded ops-qa, whose CPU,
storage, data distribution, and FIPS posture differ. The local fixture has a
similar average stored width but does not reproduce every live payload shape.

No-Regression Evidence: the reducer edit changes comments only. The touched SQL
hash operation is faster than MD5 on the controlled PostgreSQL 18.6 fixture;
the live FIPS roundtrip and changed-since reads prove the new SQL runs. Loaded
ops-qa endpoint latency is a separate rollout observation, not inferred from
the local microbenchmark.

The modified SQL keeps existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`; the stale-ref rejection has a
specific governance audit reason. No new worker, queue, index, or migration is
introduced.

No-Observability-Change: the reducer's service evidence Go MD5 fingerprint is
unchanged; only its comments now distinguish it from changed-since SQL.

## Local verification

The new FIPS SQL regression failed against all five old query constants before
the fix and passed after it. The legacy-ref HTTP regression returned 200 and
reported deletion before the fix, then returned 409 without reaching the store.
Final-tree verification uses `go test` for the Postgres, admin identity, query,
and service catalog reducer packages; `go build` and `go vet` for the touched
Go surfaces; the changed-package lint entrypoint; OpenAPI validation; strict
MkDocs build; package documentation validation after commit; and
`git diff --check`. Record their exit codes in the PR with the reviewed head.
