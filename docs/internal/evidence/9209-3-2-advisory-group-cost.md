# Advisory group computation cost (Goal #9209, Phase 3 item 3.2)

Theory proof, measured with a throwaway shim outside the repo. Refs #9209.

**Result: for the query-driven Phases 1 to 3, computing advisory groups inside
each `supply_chain_impact` run is cheap. Cost alone does not force a reducer-owned table. Whether the group id
stays stable across runs is a separate, unmeasured question, and it can force
the table anyway.**

## Decision, threshold, deadline

- Decision: is in-memory group computation inside each run affordable, or must
  groups be precomputed into a reducer-owned table?
- Started 2026-11-03T21:54:02Z (16:54:02 EST). Deadline 22:54:02Z (60 minutes).
  Run 1 was discarded for a harness bug (the CVE-only record loop never ran)
  and rerun once. That did not restart the clock.
- Threshold, set before reading the numbers:
  - group computation under 5% of the per-record decode floor on the same N;
  - under 1% of any committed whole-pass figure (the only committed whole-pass
    figure is 3.02 s in `7154-capped-scope-convergence.md`, a synthetic single
    pass for one package matching 5,000 installs, not an advisory-expansion
    pass);
  - extra heap under 5% of the 256 MiB retained-payload budget (12.8 MiB).
- The decode floor is `json.Unmarshal` of a 1.5 KB payload into
  `map[string]any`, once per record. The real run also does SQL, clone, and
  classify, so the floor understates the run.

## Sizing one run

The hosted census holds 223 `vulnerability.cve` and 239
`vulnerability.affected_package` rows
(`8880-supply-chain-impact-id-resolution.md`). Loading is key-driven by cve_id,
package_id, and advisory_id, and plan section 4.4 keeps Phases 1 to 3
query-driven. So 1x is 1,000 records (about 2.2x the 462-row census), 5x is
5,000, and 10x is 10,000. The per-intent expansion budget caps the load at
100,000 envelopes. The 100,000-member row below bounds the case where every
budgeted envelope is an advisory record in one cluster.

## What the shim does

Union-find over interned ids (one `map[string]int32`) over `aliases` edges
only. Withdrawn records are skipped. A record with more than 5 aliases is
quarantined. A group with more than 32 members is quarantined. Allow and deny
lists are 50 entries each. The group id is the lowest CVE id, else the lowest
advisory id. Each group carries the highest EPSS over its member CVEs and a KEV
flag if any member is in KEV. `upstream` edges pass enrichment down and never
merge groups. A second pass sums per-root members and enrichment.

The shim passes seed assertions and a 300-trial differential against a naive
BFS oracle. The oracle shares the rules, so it proves the implementation and
not the rules.

Assumptions the 3.2 implementation must pin, because the plan does not:

- The 32 cap counts nodes, including the CVE target (31 records plus 1 CVE is
  32, still allowed).
- The allow list exempts a group if any member is listed, and exempts a listed
  record from the 5-alias cap.
- A quarantined record keeps its own node as a singleton and contributes no
  edges. Other records can still alias it.
- A withdrawn record is skipped for its own edges. Its id is still a node if
  another record aliases it.
- A deny-listed record, and an alias target on the deny list, is dropped
  entirely.
- `upstream` pass-down raises the EPSS and KEV of the distro record's whole
  group. If that record shares a group with other members, the enrichment
  reaches them too.
- One `Compute` per pass. Up to 8 expansion rounds at 2.68 ms (10x) is still
  about 0.7% of the 3.02 s pass.
- "Lowest" id is Go byte order, so `CVE-2021-9999` sorts after
  `CVE-2021-10000`. SQL `min()` under a non-C collation can disagree. The
  parity test must pin `COLLATE "C"` or share one Go computation.

## Environment and commands

Intel i7-8700K, 12 threads, 62 GB. `GOTOOLCHAIN=go1.26.10`, matching
`go/go.mod`. Single-threaded. Host load at the start of run 2 was not
captured. The discarded run 1 started at load average 2.61.

```text
go test -count=1 ./...                                                            exit 0
go test -run '^$' -bench 'Seed|BenchmarkScale$|DecodeProxy' -benchmem -count=10   exit 0
go test -run '^$' -bench ScaleStress -benchtime=1x -benchmem -count=10            exit 0
go test -run '^$' -bench Pathological -benchmem -count=10                         exit 0
```

Spreads are benchstat 95% CIs: 1 to 5% on every row. The shim lives outside the
repo, in a session scratch directory, and is not committed.

## Seeds

| Seed | ns/op | B/op | allocs/op | Groups | Quarantined |
|---|---:|---:|---:|---:|---:|
| 30 records alias one CVE (31 members) | 3,742 | 5.6 KiB | 12 | 1 | 0 |
| DSA bundling 20 CVEs, `upstream` edges | 4,447 | 6.1 KiB | 34 | 21 | 0 |
| Same DSA as 20 alias edges | 4,110 | 6.1 KiB | 34 | 21 | 1 record |
| Cluster of 32 members (at cap) | 3,553 | 5.6 KiB | 12 | 1 | 0 |
| Cluster of 33 members | 3,646 | 5.75 KiB | 12 | 1 | 1 group |
| Record with 5 aliases | 629 | 368 B | 8 | 1 | 0 |
| Record with 6 aliases | 212 | 128 B | 4 | 1 | 1 record |
| Late alias merges 14 + 15 into 32 | 3,467 | 5.6 KiB | 12 | 1 | 0 |
| Late alias merges 15 + 15 into 33 | 3,517 | 5.6 KiB | 12 | 1 | 1 group |

Each merge row is the full recompute after the bridging record arrives, so a
merge costs the same as any other pass.

A DSA that bundles 20 CVEs must reach the reducer as `upstream` edges. As
alias edges, the 5-alias cap quarantines it.

## Scale (one pass)

| Row | Records | ns/op | ns/record | B/op | allocs/op | Groups | Quarantined |
|---|---:|---:|---:|---:|---:|---:|---:|
| 1x | 1,000 | 201 µs | 201 | 227 KiB | 491 | 473 | 1 |
| 5x | 5,000 | 1.238 ms | 248 | 1,016 KiB | 2,488 | 2,440 | 1 |
| 10x | 10,000 | 2.679 ms | 268 | 1.99 MiB | 5,076 | 4,988 | 10 |
| Stress (mirror size, not the contract) | 2,974,000 | 2.528 s | 850 | 578 MiB | 1.46 M | 1.44 M | 4,154 |

The stress row is 2.56 M OSV-style records plus 414 k CVE-only records. The
shim's alias targets add CVE nodes beyond the mirror's 414 k, so it is a harsh
superset.

## Against the decode floor

| Row | Decode floor | Group time as % | Group heap as % of decode heap |
|---|---:|---:|---:|
| 1x | 16.16 ms | 1.24% | 2.9% |
| 5x | 80.99 ms | 1.53% | 2.6% |
| 10x | 167.2 ms | 1.60% | 2.6% |

Against the committed 3.02 s pass, 2.68 ms at 10x is 0.089%. The 256 MiB
budget counts raw JSON bytes (`evidence_truncation.go`), while B/op is bytes
allocated, so the heap percentages below compare unlike units. The error is
conservative: decoded heap for that budget is far larger than 256 MiB, so the
true ratios are smaller. 10x allocates 1.99 MiB, or 0.78% of 256 MiB.

## One runaway cluster (quarantine path)

| Cluster | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| 1,000 records | 102 µs | 174 KiB | 19 |
| 10,000 records | 1.29 ms | 1.45 MiB | 89 |
| 100,000 records | 11.69 ms | 12.5 MiB | 649 |

Cost is linear at about 100 to 130 ns per member. The 100,000-member case is
0.39% of the committed pass and 4.9% of the payload budget in heap.

## Verdict

All three threshold bars pass. In-memory group computation per run is
affordable for the query-driven Phases 1 to 3.

Conditions that flip it:

- Mirror scale (Phase 5). One pass over the stress row costs 2.5 s and 578 MiB.
  Eight concurrent workers would allocate about 4.5 GiB (allocation, not live
  heap), against the 24 GiB `GOMEMLIMIT` named in the comment in
  `evidence_truncation.go`. The shard-scoped runs in plan section 4.4 (about 1,000 records
  per shard) are the 1x row. If those change, redo this proof.
- Group identity across runs. See the next section.

## What cost does not answer

The group id is the lowest CVE in a component. A run that sees only part of
the alias graph can name a group differently from another run. For example, one
run sees `GHSA-x` aliasing `CVE-2`, and another also sees `GHSA-y` aliasing
`CVE-1` and `CVE-2`. The first names the group `CVE-2`, the second `CVE-1`.
That is the second-finding defect section 4.2 is meant to remove, and it would
break the byte-identical keys across the two SQL sites.

Before implementation, the executor must settle whether the pass's
until-stable active-evidence expansion loads the full alias closure for every
CVE or advisory id it touches. If yes, compute in the pass. If no, groups must
be computed corpus-wide or stored by the reducer, for correctness and not for
cost. This is NOT_CHECKED here.

## NOT_CHECKED

- Postgres `EXPLAIN ANALYZE` for a precomputed table. There is no schema to
  explain, and the in-memory result makes the cost comparison moot.
- A typical hosted per-pass wall time. The only committed whole-pass figure is
  the 3.02 s single-shape one in the 7154 note.
- The group-id closure question above.
- The decode floor at stress size.
- Why per-record cost rises from 268 to 850 ns at stress size (cache and GC is
  a guess).

## Handoff to the 3.2 executor

1. Settle the closure question first.
2. Build a pure function over records plus enrichment: union-find over interned
   ids with a pre-sized map, then one pass per root.
3. Pin the assumptions listed above, including the byte-order and collation
   parity.
4. Emit group count, quarantine count, group-size histogram, and compute
   duration. Add a benchmark at 1x and 10x that fails above the 5% bar.
5. Make item 3.1 emit DSA bundles as `upstream`, not `aliases`. Decide whether
   the DSA's own finding takes the maximum EPSS and any KEV over all bundled
   CVEs.

No-Regression Evidence: this note changes no runtime behavior, wire contract,
or gate.

No-Observability-Change: this note adds no metric, span, log key, or status
field.
