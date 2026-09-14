# 6541 corpus timing: the directory statement at 50 repositories

The measurement record behind the corpus-timing section of
[6541: directory language query](6541-directory-query-s2.md), which carries the
summary and the `Performance Evidence:` marker. This page holds the full run:
identity, protocol, every cell, and the caveats that travel with the numbers.

Per the owner's rule the run happened on the remote Linux host. The development
machine is shared and contended, and a local figure would not be evidence.

## Identity of the run

- Remote Linux x86_64, 16 logical CPUs, 123 GiB RAM, Go 1.26.2. Backend
  `timothyswt/nornicdb-cpu-bge:v1.3.1`, manifest digest
  `sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962` —
  the #6657 pin, the same digest the local live correctness proof used.
- Corpus: 50 repositories, 20,000 Directory nodes (400 per repository, nesting
  depth 0-8), 200,000 File nodes (10 per directory, so every correct
  `file_count` is 10), seeded through the canonical projector's own write shapes
  in `go/internal/storage/cypher/canonical_node_cypher.go`, in the projector's
  phase order, 500 rows per UNWIND. Seed 122.498s; post-seed inventory
  repositories=50, directories=20000, files=200000.
- BEFORE: `origin/main` at the branch's merge-base `bb5f00671`, through
  `BuildCypherWithSemanticFilter(..., "Directory", ...)` plus the same single
  `Neo4j.Run` the shipped handler does. AFTER: `daf9f5222`, through
  `Handler.directoryRowsByLanguage` — statement, Go-side re-sort and truncate,
  and name read, the whole handler path. Production builder through the
  production reader in both arms; no hand-written Cypher in either.
- Protocol: container restarted per cell, a discarded warm-up at `limit+7`, then
  the timed run at `limit` — varying the limit is what stops the build's
  last-result cache from answering the timed run in ~1ms.
- **`absolute_target_applicable: false`.** This is NOT the accepted
  896-repository reference profile and this is not that workload, so no absolute
  target applies; every figure below is a same-machine relative comparison.

## BEFORE vs AFTER, warm timed run, exact seconds

| cell | limit | BEFORE (`bb5f00671`) | AFTER, index ABSENT | AFTER, index PRESENT | with-index vs BEFORE |
| --- | --- | --- | --- | --- | --- |
| grant-1 | 50 | 15.722s | 0.333s | **0.074s** | 212x |
| grant-1 | 200 | 16.147s | 0.279s | **0.076s** | 212x |
| grant-5 | 50 | 16.228s | 1.498s | **0.922s** | 17.6x |
| grant-5 | 200 | 16.554s | 1.609s | **0.922s** | 18.0x |
| grant-50 | 50 | 33.964s | 15.987s | **8.534s** | 4.0x |
| grant-50 | 200 | 33.544s | 16.605s | **7.265s** | 4.6x |
| unscoped | 50 | 12.484s | 15.384s | **7.465s** | 1.7x |
| unscoped | 200 | 12.664s | 15.446s | **7.603s** | 1.7x |

Cold first runs after each restart differ from the warm timed runs by well under
a second in every cell of every arm, so the cache is not what produced these.

## The index is a precondition, not an enhancement

Read the index-ABSENT column against BEFORE, not against index-PRESENT:
**without `directory_repo_id` the new shape is SLOWER than the shipped statement
at the unscoped cell** — 15.384s against 12.484s at limit 50, 15.446s against
12.664s at limit 200. The single-clause rewrite is not a win on its own at that
width; the index is what makes it one, so
`required_schema: [directory_repo_id]` on `QP-LANGUAGE-DIRECTORY` carries real
weight rather than documenting an intention.

The index was created on the populated store in 2.899s, with `SHOW INDEXES`
reporting `state:ONLINE`, `populationPercent:100`, `properties:[repo_id]`.
`readCount` stayed 0 after seeks that must have used it, exactly as the
established `nornicdb_directory_path_lookup` positive control does; the counter
is unimplemented and proves nothing either way, so the latency contrast above is
the evidence. The contrast is create-after, not drop-after: the index-absent arm
applied `origin/main`'s schema (which filters `directory_repo_id` out — 336
statements applied, 1 skipped), and the index was then created on the populated
store and every cell re-measured.

## The 10s reader deadline, unscoped, limit 50

| arm | result |
| --- | --- |
| BEFORE (`bb5f00671`) | **DEADLINE_FAILED** at 10.000s, 0 rows |
| AFTER, index absent | **DEADLINE_FAILED** at 10.010s, 0 rows |
| AFTER, index present | **INSIDE_10S_DEADLINE**, 8.325s, 50 rows |

That reproduces the issue's unscoped deadline failure and shows the branch
clears it. The caveat: 8.325s against a 10s budget on an idle 16-CPU box is
roughly 17% headroom at 50 repositories, and unscoped cost is close to linear in
grant width (grant-1 0.074s, grant-5 0.922s, grant-50 8.534s). A 100-repository
deployment would not clear this deadline. It is fixed for this corpus, not fixed
in general.

## What the tie-break keys cost

The tables above were measured at `daf9f5222`, which predates the
`ORDER BY file_count DESC, repo_id ASC, name ASC` tie-break keys that shipped in
`34a46500a`. The two heads were re-measured as a pair against one reseeded store
in one session, alternating heads, container restarted before every single run,
the identical warm-up-then-timed protocol, 2 reps per head per cell:

| cell, limit 200 | `daf9f5222` mean | `34a46500a` mean | delta |
| --- | --- | --- | --- |
| unscoped | 7.451s | 7.754s | **+0.303s (+4.1%)** |
| grant-50 | 7.746s | 7.988s | **+0.242s (+3.1%)** |

Within-head run-to-run spread was 0.614s-0.988s, larger than the delta, so at
n=2 per head the delta cannot be resolved from ordinary run-to-run variance.
Cutting the other way: the newer head was slower in **4 of 4 paired runs** and
in both cold pairs, and a consistent sign across every pair is weak evidence of
a real, small cost rather than pure noise.

So the result is a bound, not a null: **at these two cells the tie-break keys
cost at most ~0.5s (~6%), point estimate ~0.25-0.30s (~3-4%).** The data does
not support calling them free, and n=2 is too few to prove they are not.
Resolving rather than bounding it is 5 reps per head at one cell.

The `daf9f5222` figures reproduce across the two sessions — unscoped/200 7.603s
against 7.144-7.758s here, grant-50/200 7.265s against 7.303-8.188s — which is
the cross-session control on the whole exercise. The re-measure also reseeded
from scratch (119.542s against 122.498s) and reproduced the inventory exactly.

## Correctness held in every timed cell

`all_file_counts_10=true`, `within_limit=true`, and `repo_name_filled` equal to
the row count, in every timed and every cold cell of both runs and both arms.
Every directory holds exactly 10 go files, so any other count would be a nested
directory folded into a parent; the corpus nests depth 0-8 and every depth
returned its own 10.

Raw rows before the handler truncates, index present: grant-1 at limit 200
returned 200; **unscoped at limit 200 returned 10,000** (50 ids x limit 200),
`rows_not_counting_10=0`, cut to 200 by `sortAndTruncateDirectoryRows`. That is
the per-unwound-id row bound (defect 2 in the main document) measured at corpus
scale, and direct evidence that the Go-side re-sort and truncate is load-bearing
on this build rather than defensive decoration.

## The handler's two side reads — F4, closed

- `directoryRepositoryNames` at this corpus's worst case of 50 distinct
  repositories on one page: **0.005s**, 50/50 named. The recipe asked for 200
  distinct repositories, unreachable on a 50-repository corpus, so 50 is the
  true worst case measured. The issue measured 135ms for one repository; the
  bounded read is not a cost centre here.
- `allRepositoryIDs`, the unscoped caller's extra id read that F4 named as never
  timed: **0.000s**, 50 ids.

Both are negligible and the unscoped cell's 7.465s is essentially all statement,
so F4 is resolved by these two figures.

A contrast no cell in the recipe would have surfaced: a scoped caller holding no
grants at all runs BEFORE 15.730s / 0 rows against AFTER **0.000s** / 0 rows.
The shipped statement burns a full scan to prove a caller with no grants gets
nothing; the branch short-circuits before touching the backend.

## Caveats, and what is still unmeasured

1. The grant-50 raw-row probe on `34a46500a` reported 0.045s. That is a
   **result-cache hit, not a measurement** — grant-50 and unscoped resolve to
   the same 50 ids at the same limit, so the second probe re-issued a statement
   identical to the first within one container life. Its row count (10,000) and
   its count assertion are valid; its time is not, and it is not a 180x speedup.
2. The issue's 2026-09-05 grant-1/50 figure of 34.510s did **not** reproduce
   here (15.722s). The two are not a controlled pair: different machine,
   different ad-hoc seeding. The remote run reports the grant-50 cell
   reproducing closely. Everything in the tables above IS a controlled pair —
   one seeded store, one container, one machine, one session.
3. **The index's write-side cost is still unmeasured.** Every Directory MERGE
   and SET now maintains a `directory_repo_id` entry, 20,000 entries on this
   corpus at full projection. This run timed reads, not writes, so the
   projection-side delta at corpus scale remains open — named here rather than
   hidden behind a claim elsewhere.
4. Within a cell the BEFORE and AFTER arms share one container restart per arm,
   not one per run. The two arms run textually different statements, so the
   build's last-result cache cannot serve one arm from the other.
