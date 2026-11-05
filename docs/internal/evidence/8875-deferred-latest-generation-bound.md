# #8875 Round 2: Deferred Latest-Generation Partition Bound

## Scope

`ListDeferredScopedRelationshipFactRecordsQuery`
(`go/internal/storage/postgres/backfill/deferred_facts.go`) rebuilt the
latest generation for every scope on every call and kept one row: 3.7M
calls averaging ~175 ms on the primary, with the unbounded subquery
dominating estimated plan cost. The fix bounds the CTE to the query's own
scope partition, reusing the existing third bind the final join already
requires. No other text changes.

## Proof

Performance Evidence: fixture A/B on a disposable PostgreSQL 18 database
at production scale drivers (2,474 scopes, 15,892 generations, the v2
family partial index, production-width binds, ~3KB payloads; EXPLAIN
ANALYZE, BUFFERS; 3 measured runs per shape): small partitions fall from
246 to 12 shared-buffer hits (-95%) with byte-identical matched sets;
large candidate-dominated partitions are neutral (same buffers, same
rows). Keyset pagination and family predicate tightening were measured
first and rejected (no improvement). Fixture timings are host-specific
and are not a deployed wall-time claim; production confirmation follows
the live sensor after deploy.

Why safe: the bound reuses the scope parameter the final join already
requires (`latest.scope_id = fact.scope_id` with fact bound to the same
scope), so latest rows for any other scope can never join; the rest of
the statement is byte-identical. Exactness is gated by the committed
lockstep test pinning the bound plus the row-equivalence differential.
Provenance: the lockstep tests are hermetic string guards (strings/testing
only, no DSN, never skip); the row-equivalence differential was proven by a
throwaway fixture A/B against disposable Postgres during development, not by
a committed DSN-backed gate.

No-Regression Evidence: the committed lockstep tests
(TestDeferredScopedQueryBoundsLatestLookupToPartition and
TestDeferredScopedLatestLookupDerivesFromSharedCTE) pin the bound and
the shared-CTE derivation; the fixture A/B asserted byte-identical
matched sets; the backfill package suite runs green. No migration, no
DDL, no telemetry, no config change rides along.

No-Observability-Change: the fix adds no signal. Deferred-load latency
is observed through the existing backfill/reducer gauges; a dedicated
sensor was not added because the change is predicate-only.
