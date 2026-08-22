# #6208 runner lease-hold live theory evidence

## Scope and dependency boundary

This record proves the proposed `runner_lease_hold` fault mechanism against the
real shared-projection lease path before production cell code is written. It is
a diagnostic correctness proof, not a runtime change or a claim that the final
Ifá family cells pass.

The three target family fixtures for `handles_route`, `runs_in`, and
`invokes_cloud_action` are owned by prerequisite issues #5995, #6000, and
#5997. They were not merged when this proof ran, so the shim deliberately did
not copy their fixtures or edit their coverage rows. It used one schema-valid
synthetic `handles_route` refresh intent scoped to the committed code-call
cassette's real acceptance identifiers. The finished cells must replace that
synthetic row with each family's real authored intent and graph oracle.

The proof ran at source commit
`b635b327d106a31e19955a696dccc74e42759da1` with production host binaries,
PostgreSQL 18, and the Compose-pinned NornicDB image
`eshu-nornicdb-pr290:3722b483c02c`. The recorded run identifier is
`runner-lease-hold-20260822T103129Z`.

## Flow and failure boundary

The relevant path is:

```text
CodeCallMaterializationHandler
  -> shared_projection_intents
  -> SharedProjectionRunner
  -> ProcessPartitionOnce
  -> ClaimPartitionLease
  -> pg_advisory_xact_lock(namespace, projection_domain)
  -> shared_projection_partition_leases upsert
  -> select/write/complete work
```

`ClaimPartitionLease` takes the transaction-scoped advisory lock before it
inserts or renews a lease row. The fault holder therefore opens a separate
transaction and takes the exact production key for the target domain. Killing
the reducer client does not immediately cancel PostgreSQL backends already
waiting on that key. After the holder releases, those orphaned server requests
can finish the lease statement and commit lease rows even though no reducer
process remains to select intents or write graph state. A replacement reducer
therefore starts only after those waiters drain, then retries the whole
`ProcessPartitionOnce` boundary from durable intent state. The mechanism does
not change intent IDs, completion semantics, lease TTLs, retry policy, or
worker count.

The following alternatives are not acceptable substitutes:

- table locks or a lock broader than the production advisory key;
- worker-count reduction, batch size one, or another serialization control;
- an advisory lock whose namespace or domain differs from
  `ClaimPartitionLease`;
- sleeping without proving that a production runner is waiting on the lock;
- declaring success from a pending intent alone or a waiter alone;
- query-text matching as the identity of the waiting claim statement.

## Live method and observations

The ignored throwaway shim built `bootstrap-data-plane`, `ifa`, `projector`,
and `reducer`, started fresh Compose PostgreSQL and NornicDB services, and
drove `testdata/cassettes/codecalls/ifa-code-call-family.json`. The drive
created real work and six `code_calls` shared intents. The shim then inserted
the single synthetic target refresh, armed the exact `handles_route` advisory
key, and started the production projector and reducer with four shared
projection workers. It retained controller output plus the negative,
positive, post-kill, pre-replacement, and post-replacement database snapshots
under the recorded run identifier.

The lock observation joined a granted holder row to an ungranted waiter row in
`pg_locks` on `locktype`, `database`, `classid`, `objid`, and `objsubid`. It
also required the holder's unique `application_name` and the waiter's
`pg_stat_activity.wait_event_type = 'Lock'`. PostgreSQL documents that a
two-integer advisory lock stores the first key in `classid`, the second in
`objid`, and uses `objsubid = 2`; an ungranted `pg_locks` row is a waiting
request. The holder itself was independently proven from another session with
`NOT pg_try_advisory_xact_lock(...)`, avoiding signed hash-value comparisons.
The run's exact lock tag was `locktype=advisory`, `database=16384`,
`classid=1155623211`, `objid=3401466860`, and `objsubid=2`. The negative
snapshot had only the granted holder; the positive snapshot had that same
holder plus four ungranted `Lock/advisory` waiters on the tag.

The runnable wait predicate was the conjunction of:

1. at least one pending intent for the target projection domain; and
2. at least one production connection waiting on the holder's exact granted
   advisory lock tag.

Each poll issued a fresh, fast SQL statement with a five-second statement
timeout. A single long polling statement was rejected because PostgreSQL can
retain one statement's activity/statistics snapshot while the concurrent
runner state changes. Filtering by the waiter's query text was also rejected:
the exact advisory lock tag and wait state are the ownership contract.

| Observation | Result |
| --- | ---: |
| Holder armed before reducer start | `waiter_count=0`; predicate rejected in 225 ms |
| Reducer started with pending target | `pending_target=1`; `parked_reducers=4`; predicate accepted in 1,862 ms |
| Independent `code_calls` control during hold | already `6` total, `0` pending at the positive snapshot; confirmation query returned in 225 ms |
| Target state during control | still pending and still parked |
| Holder-client lifecycle interval | 5,499 ms, including startup and teardown overhead |
| Reducer kill and join | process gone; four PostgreSQL waiters remained |
| Release to first durable lease row observation | within 497 ms; four orphaned claims committed before replacement |
| Replacement to all partition-row observation | within 356 ms; lease rows increased from four to eight |
| Target state after replacement | eight released lease rows; one synthetic intent still pending |

The kill/reclaim oracle is ordered: the reducer process was killed and joined;
the four server-side waiters remained visible; terminating the holder let those
requests commit four active lease rows; the waiters then drained; and a newly
started reducer inserted rows for the other four partitions. A later snapshot
showed all eight rows released, proving that the replacement also reclaimed
and released the original four. The synthetic target remained blocked by
missing semantic-readiness evidence, so completion would be a false oracle at
this dependency stage. This proves client death, orphaned claim completion,
and replacement progress at the lease/runner seam. It does not prove target
graph convergence.

## Known collateral and final implementation contract

The proof intentionally exposed the mechanism's collateral: all four shared
projection workers parked on the same domain key. Because one concurrent cycle
queues the eleven shared-runner domains in domain order, this fault can stall
the entire `SharedProjectionRunner` for the duration of the hold, not just the
target family. The dedicated `code_calls` runner remained independent and
completed all six control intents. Final cells and coverage prose must name
that whole-runner stall explicitly.

Once the prerequisite fixtures merge, implementation must be test-first and
must:

- add the `runner_lease_hold` vocabulary, classifier, registry rows, and
  registry-to-Go lockstep checks;
- acquire the production lease key only after the target family has authored
  a pending intent;
- require the exact pending-intent-plus-waiter predicate before killing the
  reducer;
- kill and join the reducer, release the holder, wait for orphaned PostgreSQL
  claim requests to drain, and only then start the replacement;
- terminate and join the holder client cleanly on success and every failure;
- prove every real family converges to its exact edge set and digest with zero
  nonterminal first-stage work and zero first-stage dead letters;
- register the generic and family-specific cells in their shards, atomic
  groups, CI triggers, selector mirror, coverage prose, and gap retirement;
- rederive the registry totals instead of copying stale counts.

Unit and hermetic shell tests must cover the positive shape plus missing
`IntentWriter`, wrong handler stage, wrong advisory key, non-shared domains,
negative observation before reducer start, holder release, wait ordering, and
timeout cleanup. The promoted live cells must then rerun this same contention
proof with the committed family cassettes and graph assertions.

## No-Regression Evidence:

No production code or configuration changed in this design-and-shim slice.
The exact live wait predicate rejected the negative control in 225 ms and
accepted the real four-worker contention state in 1.862 seconds, below its
five-second bound. The independent `code_calls` lane was already fully drained
at the positive snapshot; its later confirmation query returned in 225 ms.
The holder-client lifecycle lasted 5.499 seconds including startup and teardown
overhead. The first orphaned lease row was observed within 497 ms of release.
Once all orphaned waiters drained, all eight rows were observed within 356 ms
of replacement startup, and a later snapshot showed all eight released. The
final tracked implementation still requires its focused tests, live family
cells, performance-evidence gate, and the repository promotion gate.

## Observability Evidence:

The proof used existing production state only: durable
`shared_projection_intents`, `shared_projection_partition_leases`,
`pg_locks`, `pg_stat_activity`, and reducer structured logs. The final cells
can also use the existing shared-projection completion, processing-duration,
lease-quarantine, backlog, and structured-log signals. There is no dedicated
advisory-wait metric and no shared-intent attempt/dead-letter lifecycle;
dead-letter assertions apply to the first-stage `fact_work_items` queue. No
metric, span, log key, status field, runtime knob, or production query was
added by this proof.

PostgreSQL references:

- [The `pg_locks` view](https://www.postgresql.org/docs/current/view-pg-locks.html)
- [`pg_stat_activity` and statistics snapshots](https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-ACTIVITY-VIEW)
