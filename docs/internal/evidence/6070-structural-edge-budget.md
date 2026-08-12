# Issue #6070 — structural-edge phase-group statement budget

Validation record for giving the `structural_edges` canonical write phase its
own statement budget.

## What was broken

Four repositories on the 896-repository profile never got their
`File-[:IMPORTS]->Module` edges into the graph. Two exhausted the 120s canonical
write budget; two failed `TransactionCommitFailed` at 64.6s and 86.1s, which
says the transaction was too large rather than merely too slow.

PR #5911 wired the IMPORTS producer that #5691 recorded as missing. That is a
correct accuracy fix and stays. It moved real work into a phase that had almost
none: `structural_edges` went from 2 to 601 phase-group chunks corpus-wide while
fact volume did not grow, because the imports were always parsed and simply never
projected.

The chunker could not see it. `structural_edges` had no narrow phase budget, so
it fell through to `DefaultPhaseGroupStatements` (500) while each of its
statements carries up to the canonical writer's batch size (500) rows. The worst
scope, `r_962c9686`, was 147 statements — far under the statement cap, and
roughly 73,500 rows in one transaction.

## Comparison

Both runs used the 896-repository corpus, NornicDB image
`eshu-nornicdb-pr290:3722b483c02c`, clean volumes, the same Compose override, and
`ESHU_CANONICAL_WRITE_TIMEOUT=120s`.

| Signal | Baseline `311bdc563` | With the budget `c2c801b72` |
| --- | --- | --- |
| `context deadline exceeded` / `TransactionCommitFailed` | 4 scopes | 0 |
| Dead-lettered scopes | 4 | 0 |
| Worst scope `r_962c9686` transaction shape | 1 transaction, 147 statements, ~73,500 rows | 30 transactions, `chunk_count=30` observed 30 times |
| Compose exit code | 1 | 0 |

`147 / 5 = 30`, so the observed split is exactly the one the budget predicts. The
worst scope appears 30 times in the run log, so the zero-failure result is not a
zero-by-never-reaching-it.

The eight `dead-letter` string matches in the run log are stage lines for a
corpus repository named `dead-letter`, not failures.

## What this evidence does not establish

- **No wall-clock speedup is claimed.** Wall time was 1023s against the
  baseline's 1643s, but both runs abort when `bootstrap-index` exits, and the
  measured run was killed (137) with 8,956 queue items still pending. The two
  runs completed different amounts of downstream work, so the totals are not
  comparable and the difference is not a win.
- **Neither run is queue-terminal.** The comparison axis is the graph-write
  failure signatures in the run logs, which is the same source the original four
  dead letters came from — not queue drain.
- `main` moved between the baseline commit and this branch's base. That drift
  touches Postgres AWS runtime-drift evidence only, nothing on the canonical
  graph write path, so a single run attributes the change cleanly.

## Why 5

Structural-edge statements are row-batched at the canonical writer's batch size,
so transaction pressure is batch size times statement cap: 5 x 500 = 2,500 rows.
That is the reference grouped row pressure documented in
`docs/public/reference/nornicdb-tuning.md`, and it matches the existing
`files=5` and `directories=5` budgets. The remote run above is what confirms the
number holds on a large graph; before it, 5 rested on precedent alone.

A per-transaction row cap was considered and rejected. This codebase expresses
row bounding as batch size times statement cap, never as a transaction row
counter. A statement cap bounds the worst case here because every row-carrying
family in this phase goes through `buildBatchedStatements` at the writer batch
size. The row cap's one theoretical advantage is void: a chunk-boundary row cap
cannot split a single oversized statement either, so both designs carry the same
residual risk for the unbatched family-edge builders (Atlantis, Flux, GitLab,
Helm, Kustomize), which emit a handful of rows in practice. The row cap would
also have had to change `executeGroupedChunksObserved`, which the entity path
reaches by delegation.

## Safety of chunking this phase further

Verified before the change rather than assumed:

- Each chunk is already its own transaction
  (`phase_group_executor.go`, `executeGroupedChunksObserved`), and
  `PhaseGroupExecutor` deliberately does not implement `GroupExecutor`, so the
  atomic path is dead in production. #3483 records that as intended, with
  production relying on MERGE idempotency plus retry.
- Nothing in the phase MATCHes a node another statement in the same phase
  MERGEs; every endpoint comes from an earlier, already committed phase.
- The one real ordering rule — a retract before its own family's MERGE — is
  enforced upstream by `executeGroupedChunksWithDrain`, which flushes pending
  merges before each retract and runs retracts in emitted position. Narrowing
  500 to 5 subdivides merge runs and cannot reorder them.

`bootstrap-index` duplicates the chunker and had the identical defect, so it
carries the same budget and the same environment override.

Performance Evidence: remote 896-repository run
`6070-structural-edge-budget-20260812T141256Z` at commit `c2c801b72` against
baseline `5122-post5222-head-t120-20260811T195642Z` at commit `311bdc563`, with
the comparison table and its stated limits above.

Observability Evidence: no new metric, span, or log line. The existing
`nornicdb phase-group chunk completed` line already reports `chunk_index`,
`chunk_count`, `statement_count`, and `duration_s` per transaction, and it is
what carried the `chunk_count=30` reading above.
