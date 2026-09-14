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

## The index's write-side cost, measured — a bound, not a null

The run above timed reads, not writes. A separate remote run
(`6541-directory-write-cost-20260914T122238Z`) closes that gap.

**Design.** The run was taken at Eshu head
`5a1292b7392345eba129b2ae53603dce58aff1dd`. The harness copies the canonical
Cypher constants verbatim from
`go/internal/storage/cypher/canonical_node_cypher.go` and never runs the
projector Go path, so what it measures is fixed by those constants and by the
one index DDL statement the two arms differ in; nothing else in the tree moves
the write shapes it timed. These figures therefore describe shipped behaviour
under a condition a reader can test at any head, rather than under a commit
distance that every later commit invalidates. The condition: that constants
file must still be the blob it was at the measured head,
`01bd07e32c2f0e45ca7437d052bf222edad6a574`, and the single `CREATE INDEX`
statement naming `directory_repo_id` in
`go/internal/graph/schema_tables_indexes.go` must still be the statement the
index-present arm applied. Test the first with `git rev-parse
HEAD:go/internal/storage/cypher/canonical_node_cypher.go`, and the second with
`git diff 5a1292b7392345eba129b2ae53603dce58aff1dd..HEAD --
go/internal/graph/schema_tables_indexes.go`, whose hunks must be comment lines
only. Both held when this paragraph was written. Six
reps per arm, run alternating (absent 1, present 1, absent 2,
present 2, …) so machine drift cannot land on one arm; a fresh container and a
fresh volume per rep; the only difference between the two arms is dropping the
one DDL statement containing `directory_repo_id`. Same corpus recipe and the
same backend image digest as the read-side run above — 50 repositories /
20,000 directories / 200,000 files, seeded through the canonical projector's
own write shapes in phase order, 500 rows per UNWIND. The index is created
**before** the writes it is charged for, so this measures steady-state
maintenance, not a backfill. **`absolute_target_applicable: false`** — not the
accepted 896-repository reference profile, so every figure is a same-machine
relative comparison.

### Directory-node phase — the phase that writes `d.repo_id`

| | index absent | index present |
| --- | --- | --- |
| mean of 6 reps | **1.537s** | **1.525s** |
| min–max | 1.519–1.559s | 1.494–1.544s |
| stdev | 0.018s | 0.017s |

Whole projection, mean of 6: absent **49.442s**, present **49.422s**.

### It does not resolve

The point estimate is negative in four of the five measured phases and in the
total (only `files`, at +0.009s, is positive), which is what noise looks like,
not what a cost looks like. So the honest figure is the **upper 95% CI
limit**, read as a bound — the most the index could be costing and still be
consistent with these samples:

- **Directory-node phase: at most +10.3 ms across 20,000 Directory MERGE/SET
  rows — at most +0.52 µs per directory write, at most +0.67% of the phase.**
- **Whole projection: at most +396 ms on 49.4s — at most +0.80%.**

Resolving the directory-node delta would need ~33 reps per arm, the
whole-projection delta ~4,200. Neither is worth running: the bound is already
an order of magnitude tighter than any decision that rests on it. This is a
bound and not a null, and nothing here licenses reading it as "no measurable
cost" or as "free".

### Per-batch slope — maintenance is not degrading as the index fills

With the index present the directory-node per-batch slope is **−1.1e-5 s/batch
across 40 batches**: the 40th batch of 500 directories is no slower than the
first even though 19,500 entries are already in the index by then, and total
drift across the phase is under 0.5 ms. The `files` phase does carry a small
positive slope, +3.4e-5 s/batch — but it is identical to six significant
figures in both arms, so that is the graph growing under 200,000 File MERGEs,
not this index.

### The trade

Performance Evidence: write-side cost of `directory_repo_id` at corpus scale,
remote Linux host, NornicDB `v1.3.1` at digest
`sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962`, 50
repositories / 20,000 directories / 200,000 files, 6 reps per arm with a fresh
container and a fresh volume per rep, `absolute_target_applicable: false`. The
canonical directory-node phase ran 1.525s with the index present against 1.537s
absent, and whole projection 49.422s against 49.442s; the delta does not
resolve against the ±0.05s within-arm spread, so the cost is reported as a
bound — at most +0.52 µs per directory write, at most +0.67% of that phase, at
most +0.80% of corpus projection. What that bounded write cost buys on the read
side is the index-ABSENT to index-PRESENT step of the table above, not the
BEFORE→AFTER total: 4.5x at grant-1/50 (0.333s -> 0.074s), 1.6x at grant-5/50
(1.498s -> 0.922s), 1.9x at grant-50/50 (15.987s -> 8.534s), 2.1x at
unscoped/50 (15.384s -> 7.465s) and 2.0x at unscoped/200 (15.446s -> 7.603s) —
a read-side saving of roughly 0.26s to 7.9s depending on grant width, against a
write-side bound of at most +396 ms on a 49.4s projection. At the unscoped cell
the index is not an optimisation on top of the rewrite: it is what brings the
statement inside the 10s deadline at all (index absent DEADLINE_FAILED at
10.010s, index present 8.325s with 50 rows). The 15.722s -> 0.074s pair at
grant-1/50 is the rewrite-plus-index total against `origin/main`'s shipped
statement, not the index's share of it.

## Caveats

1. The grant-50 raw-row probe on `34a46500a` reported 0.045s. That is a
   **result-cache hit, not a measurement** — grant-50 and unscoped resolve to
   the same 50 ids at the same limit, so the second probe re-issued a statement
   identical to the first within one container life. Its row count (10,000) and
   its count assertion are valid; its time is not, and it is not a 180x speedup.
2. **Neither** of the issue's 2026-09-05 figures for the replaced statement
   reproduced here: grant-1/50 34.510s there against 15.722s here (2.20x),
   grant-50/50 121.437s (2m01.437s) there against 33.964s here (3.58x). That
   measurement and this one are not a controlled pair: different machine,
   different ad-hoc seeding. `report-remote6541.md` §1 closes by reporting the
   grant-50 cell as reproducing closely; that is wrong and is not carried here
   — it matches 33.964s (grant-50 measured here) against 34.510s (the issue's
   **grant-1** figure), two different cells that agree to 1.6% by coincidence.
   Everything in the tables above IS a controlled pair — one seeded store, one
   container, one machine, one session.
3. **The index's write-side cost is measured, but as a bound.** Every Directory
   MERGE and SET maintains a `directory_repo_id` entry, 20,000 entries on this
   corpus at full projection. The run above did not time those writes; the
   separate write-cost run in the section before these caveats does, and its
   delta does not resolve — so the cost stands as "at most +0.52 µs per
   directory write", never as zero.
4. Within a cell the BEFORE and AFTER arms share one container restart per arm,
   not one per run. The two arms run textually different statements, so the
   build's last-result cache cannot serve one arm from the other.
