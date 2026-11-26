# #11520 H2 hot-id holder-read isolation: current vs proposed pass diagram

Date: 2026-11-26.

Refs #11520. The [clearance cohort](11520-clearance-cohort-2026-11-26.md)
names the cheapest shim (EXPLAIN ANALYZE BUFFERS plus a bounded
isolated-vs-shipped replay); the guard proof caps the worst holder id under
10s and the saturation shim caps handler service time.

## Summary

A hot singleton burning toward the 10s guard head-of-line-blocks the pass's
cold packs under one shared semaphore of 4. H2 routes hot singletons down an
isolated lane under the same total bound, with identical statement text,
guards, fences, and admitted set — scheduling decoupling, not a cheaper plan.

Binding constraints (from #11520 Q3): no timeout raise, no worker/batch cuts,
no batch-size-1, 10s guard stays, claim/key fences and admitted set untouched
(every id in exactly one statement), parallelism bound 4 unchanged.

## (a) CURRENT hot-id pass

Source bodies: the MATERIALIZED 3-arm holder query (10s guard), the
plan/chunk/fence helpers in `go/internal/reducer/supplychain/core/`
(10k-row hot-mass threshold, bound 4, chunk 5, cap 256, 8-round
until-stable loop).

```mermaid
flowchart TB
    commit["writer commit<br/>sets fanout_owed in-txn"] --> fanout["fanOutHolders<br/>fanout.go"]
    fanout --> inflight["in-flight read FIRST<br/>FindInFlightNonMarkerScopes<br/>#10972 D4b"]
    fanout --> chunk["chunk lookupIDs by 5<br/>fanOutHolderReadChunk=5"]
    chunk --> probe["mass probe per id<br/>EstimateHolderMasses<br/>same 10s guard"]
    probe --> plan["planHolderReadStatements<br/>hot gt 10k rows or unknown mass → singleton<br/>cold pack: ≤5 ids AND ≤10k combined mass<br/>EVERY id in exactly one statement"]
    plan --> split["readHolderScopesSplit<br/>1 statement → direct call<br/>N statements → bounded parallel<br/>sem cap 4, union under mutex<br/>first error wins, fail CLOSED"]
    split --> stmt["ONE shipped statement each<br/>FindSupplyChainImpactHoldersQuery<br/>MATERIALIZED 3-arm UNION<br/>PRIMARY, read-only RR txn<br/>statement_timeout 10s<br/>supports-ready gate per txn"]
    stmt --> union["union scopes<br/>minus asking scope, sorted"]
    union --> prefilter["dropQueuedHolders<br/>FindQueuedHolderScopes<br/>BEFORE cap slice"]
    prefilter --> reopen["reopen loop, serial<br/>cap 256, rest → continuation list"]
    reopen --> state["UpdateFanOutState<br/>OWN item only, lease-fenced<br/>lease owner + live lease + attempt"]
    state --> ack["queue ack"]
    ack --> evload["reopened holder pass<br/>evidence load until-stable<br/>≤8 rounds, then expansion"]
```

Pool/fence facts: each holder statement checks out a pooled connection for
one read-only transaction (timeout scoped, readiness check, query,
ROLLBACK). One slow hot singleton holds its connection plus snapshot to the
10s guard; the saturation shim read the primary at 96% of request.

## (b) PROPOSED isolated pass (H2)

Same admitted set, same statement text, same guards. The ONLY routing change:
hot singleton statements run in an isolated lane with its own labeled
boundaries, so a hot statement burning toward the guard cannot head-of-line
the cold packs of the same pass. Statement count, statement text, guard,
fences, cap, and downstream load are identical.

```mermaid
flowchart TB
    commit["writer commit<br/>sets fanout_owed in-txn<br/>UNCHANGED"] --> fanout["fanOutHolders<br/>UNCHANGED"]
    fanout --> inflight["in-flight read FIRST<br/>UNCHANGED #10972 D4b"]
    fanout --> chunk["chunk lookupIDs by 5<br/>UNCHANGED"]
    chunk --> probe["mass probe per id<br/>same 10s guard<br/>UNCHANGED"]
    probe --> plan["planHolderReadStatements<br/>SAME partition invariant:<br/>every id in exactly one statement<br/>UNCHANGED"]
    plan --> cold["COLD lane<br/>packed statements<br/>≤5 ids, ≤10k mass<br/>shipped statement text<br/>PRIMARY pool"]
    plan --> hot["HOT lane ISOLATED<br/>singleton statements only<br/>hot gt 10k or unknown mass<br/>SAME shipped statement text<br/>SAME 10s guard each"]
    cold --> poolC["BOUNDARY: POOL checkout<br/>shared pass budget"]
    hot --> poolH["BOUNDARY: POOL checkout<br/>shared pass budget<br/>total ≤ 4 over BOTH lanes"]
    poolC --> tx["BOUNDARY: TX per statement<br/>read-only RR + set_config timeout<br/>supports-ready gate"]
    poolH --> tx
    tx --> union["union scopes, minus asker<br/>IDENTICAL set as today"]
    union --> prefilter["dropQueuedHolders<br/>BEFORE cap slice<br/>UNCHANGED"]
    prefilter --> reopen["reopen loop, serial, cap 256<br/>UNCHANGED"]
    reopen --> lease["BOUNDARY: LEASE fence<br/>own item, owner + live lease + attempt<br/>UNCHANGED"]
    lease --> ack["queue ack<br/>UNCHANGED"]
    ack --> evload["reopened holder pass<br/>≤8-round until-stable load<br/>UNCHANGED"]

    subgraph claimfence["BOUNDARY: CLAIM fence (untouched)"]
        direction LR
        cf["representative + live-lease fence<br/>admits everything: max 1–2 rows/key<br/>no reordering, no batch change"]
    end
    claimfence -.-> fanout
```

What "isolated" means: hot singletons already run as separate statements
today; H2 adds lane separation under the SAME total bound of 4, so cold
packs complete while a hot statement burns its guard. It adds no
connections, raises no guard, reorders no claims, and widens no caps.

## (c) Exact starvation/concurrency risks and the cheapest-shim measurements

### Risks (each names the mechanism, not a vibe)

1. Hot pins a slot: a hot singleton near the guard holds a pooled
   connection, snapshot, and sem slot while cold statements wait. H2 must
   decouple scheduling WITHOUT exceeding 4 total per pass.
2. Retry amplification: any statement error fails the whole pass CLOSED and
   retries everything. An isolated hot failure must still fail the pass,
   never commit a partial union.
3. Error masking: only the first error is recorded. Isolation must keep
   per-lane read kinds/counters so the operator sees which lane failed.
4. Probe cost: the mass probe is one guarded statement per chunk. H2 adds
   scheduling, never probe statements.
5. Saturation ceiling: the primary sits at ~96% of request. H2 claims NO
   write-path relief; measure read-phase only.
6. Member-arm overestimate is safe-direction (splits MORE, never less). H2
   must preserve that direction for unmeasured ids.
7. Order invariants: in-flight read BEFORE row reads, prefilter BEFORE cap
   slice, reopen AFTER union, lease-fenced state update. H2 moves none.
8. Claim fence untouched: the DROP precedent (#10972 Box-2) forbids
   smuggling claim reordering into a read-isolation change.

### Cheapest-shim measurements (prove-theory-first, in order)

1. EXPLAIN ANALYZE BUFFERS at production shape: worst holder id, both
   plans, read-only replica. Bars: medians ≤ 5.0s, no run ≥ 10s, else STOP.
2. Bounded replay of ONE hot-id pass, isolated-vs-shipped, with findings
   identity (union == single == fact oracle). Bar: PROCEED only at phase
   ratio ≥ 2.0, else STOP with no implementation.
3. Contention cut only if (1)+(2) pass: ≥ 10min vuln-filtered pass sample
   (hot/cold p95 vs today, conn wait, zero timeouts), no knob moves.

Out of scope: worker bumps, file-fold index (#11093), criteria-expansion
bound (#11087), intake throttling, tier extension.
