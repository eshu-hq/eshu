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
Page through the mapping list with `next_after_ref` as `after_ref` to obtain
current refs even beyond the 500-row page limit; the underlying mapping rows
and OIDC group resolution are unchanged. Ops-qa had zero rows in
`identity_provider_group_role_mappings` at the time of this read-only check.
Other deployments may have rows, so the client transition is documented in the
HTTP API and OpenAPI. Unknown non-legacy refs retain the existing idempotent
no-op behavior. Tenant/workspace predicates and terminal-state guards on delete
are unchanged.

Changed-since still groups by category and stable fact key. A key with one
active payload compares its SHA-256 digest directly. A duplicate-key group
first compares row count and minimum digest; when both match, it compares the
sorted multiset of every payload digest so a changed non-minimum payload or
multiplicity cannot be called unchanged. Counts and samples use the same
classification CTE. Tombstones are not hashed, and `retired` and `superseded`
retain their precedence. The digest remains `bytea` to avoid per-row hex
encoding. This is a correctness change from the old `MIN(md5(...))` behavior,
which could miss duplicate-group differences even without FIPS.

## Isolated ops-qa proof

The changed-since and initial mapping-list checks used read-only transactions
or `SELECT`. A later roundtrip used a transaction-scoped temporary table with
`search_path = pg_temp` and a five-second statement timeout. It rolled back and
confirmed the temporary table was removed. No application rows were written.

- `md5('foo')` and the old `MIN(md5(payload::text))` failed with the FIPS error.
  `encode(sha256(convert_to('foo', 'UTF8')), 'hex')` returned a 64-hex value.
- The initial SHA-256 `changedSinceCountsQuery`, extracted from the source, ran
  against the same 158-row generation as both prior and current. It returned
  8 unchanged content entities, 147 unchanged facts, and 3 unchanged files.
  The exact revised samples query returned two ordered fact handles.
- The initial SHA-256 mapping-list query, given one synthetic row through a CTE,
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

Additional local PostgreSQL 18.6 proof used two 158,525-row synthetic
generations with an average stored JSONB payload width of 1,047 bytes. The
source-extracted final count query took 396 and 371 ms, compared with 342 and
336 ms for the simpler SHA-256 minimum-digest query on the same mostly-unique
fixture (about 10–17% slower). A duplicate-heavy fixture of the same size took
782 and 680 ms with the candidate multiset query. These are isolated SQL
measurements, not endpoint latency or loaded ops-qa measurements. Ops-qa was
unreachable for a final live timing read after this change; rollout observation
is still required.

Performance Evidence: a real-Postgres regression covers changed non-minimum
payloads, reordered equal multisets, multiplicity changes, singleton updates,
and tombstones. The changed query is measurably slower than the simpler SHA-256
query on the controlled mostly-unique fixture; this cost buys the exact
duplicate-key classification required by the changed-since contract. The
reducer edit changes comments only.

The golden-corpus verifier also now hashes each payload once in materialized
row CTEs before sorting the digest arrays. On the same local PostgreSQL 18.6
158,525-row prior-generation fixture, its former aggregate expression took
438.539 and 395.007 ms; the materialized hash-once form took 264.349 and
264.542 ms in alternating runs. This measures verifier SQL only, not B-7 wall
time. A seeded four-call SQL-log assertion failed before the helper change and
passed with two SHA-256 call sites afterward. The final B-7 hash-once rerun
passed 560 required checks on 31 repositories with zero required failures
and two advisory timing warnings in 199 seconds.

The modified SQL keeps existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`; the stale-ref rejection has a
specific governance audit reason. No new worker, queue, index, or migration is
introduced.

No-Observability-Change: the reducer's service evidence Go MD5 fingerprint is
unchanged; only its comments now distinguish it from changed-since SQL.

## Local verification

The new FIPS SQL regression failed against all five old query constants before
the fix and passed after it. The real-Postgres duplicate-payload regression
failed against the prior SHA-256 minimum-digest query and passed with the
multiset classifier. The mapping-list pagination regression failed before the
cursor change and passed afterward. A separate real-Postgres fixture proved
500 and 1 rows across two pages while excluding a different tenant and a
tombstoned mapping. The B-7 helper seeded-MD5 guard failed before the helper
was updated and passed afterward. The legacy-ref HTTP regression returned 200 and
reported deletion before the fix, then returned 409 without reaching the store.
Final-tree verification uses `go test` for the Postgres, admin identity, query,
and service catalog reducer packages; `go build` and `go vet` for the touched
Go surfaces; the changed-package lint entrypoint; OpenAPI validation; strict
MkDocs build; package documentation validation after commit; and
`git diff --check`. Record their exit codes in the PR with the reviewed head.
