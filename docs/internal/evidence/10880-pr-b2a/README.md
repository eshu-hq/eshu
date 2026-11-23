# #10880 PR-B2a theory proof: unscoped existence-filter reads (route V)

Refs #10880. Scratch SQL behind the unscoped existence-filter reads that the
arbiter ruled for PR-B2a (`.claude/arbiter-b2a.md`, ruling V). It is evidence,
not a migration: nothing here is applied by the product, and all data is
synthetic. Governing rule: the root AGENTS.md "Prove-The-Theory-First" section
and [Investigation Budget](../../agent-investigation-budget.md). The ruling
requires this proof before any build and before the migration PR.

## Decision, threshold, deadline (stated before measuring)

- **Decision.** Whether the V shapes can be built as ruled. V serves unscoped
  `profile=precise` from the shipped winners read (branch A, precise predicate on
  the winner row) plus a supplement (branch B): the winners rows that match the
  key-determined selectors and fail the precise predicate probe their best
  precise-eligible support through a new partial `(canonical_key)` index, in one
  statement, `UNION ALL`. It serves unscoped `image_ref` from the supports table
  through a new partial `(image_ref)` index. The two reads are measured here on
  the real bootstrapped schema, with the real winners table and the real supports
  table and its five shipped indexes.
- **Threshold (the ruling's bar).** At 80,000 canonical keys x 100 supports
  (8,000,000 supports) plus one package with at least 1,500,000 supports, a key
  with 281 precise supports, and a case where every winner fails precise: every V
  read (list, count with no selector, the five inventory dimensions, grouped
  inventory) and the image read finish in under 1,000 ms, warm, with the server's
  default JIT setting. Warm is the median of five runs after one warm-up. A read
  over the bar means the shape is rejected and the question returns to the
  arbiter; nothing is built on it.
- **Cheapest existing test.** The arbiter's own shim measured V at 116 to 290 ms
  on a lean table. This proof repeats the shape on the real schema, which the
  ruling lists as `NOT_CHECKED`: the active-generation and classification joins,
  the operator override probe, the payload fetch, and the inventory and grouped
  statements.
- **Start / deadline / attempts.** 2026-11-23T05:28Z start; deadline
  2026-11-23T08:30Z (three hours, preparation and data generation included); one
  theory (V and the image read), at most two attempts. A failed attempt does not
  restart the clock.
- **Out of scope.** The writer WAL measurement (ruling proof 8) needs the real
  writer harness and is a separate step after the indexes are cut. The QA census.

## Environment

PostgreSQL 18.6 (Ubuntu package) in a private cluster, `shared_buffers=2GB`,
`work_mem=16MB`, `jit` at the server default (on), `max_parallel_workers_per_gather`
at the default. The schema comes from `ApplyBootstrap`, so the table definitions,
indexes and constraints are the shipped ones.

## Files

| File | What it is |
| --- | --- |
| [RESULTS.md](RESULTS.md) | The measured numbers, findings and `NOT_CHECKED` list. |
| `gen_v_corpus.sql`, `gen_extras.sql`, `gen_profile_patch.sql` | Build the 8M-support corpus, the repository-to-service map and hot image, and the writer-style profile stamping, in that order. |
| `gen_skew_corpus.sql` | The QA-like skewed corpus generator (not run). |
| `build_v_reads.py`, `run_v.sh` | Write the V and image statements; time them. |
| `v_indexes.sql` | The two partial indexes (the ordered form that passes is in RESULTS.md). |
| `candidate_profile.sql`, `.out` | Branch B candidate counts. |
| `worst_case_all_winners_fail.sql`, `restore_winners.sql` | The every-winner-fails case and its undo. |
| `wal_raw.txt`, `wal_summary.py`, `wal_summary.txt` | Real-writer WAL results and their summary. |
| `bootstrap_index_check.txt` | A full `ApplyBootstrap` on a throwaway PostgreSQL 18.6: both indexes are `indisvalid`, `indisready` and `indislive`, with their definitions. |
| `explain_ordered_probe_v_list_status.txt`, `results_*.tsv` | Raw plan and timing rows. |

The real-writer harness (`zz_b2a_wal_scratch_live_test.go`) is not committed; it is the B1 m6
harness shape with the index variants of this proof.

## What each guard pins

Migration 343's header says the Go constant `impact.SupportsPreciseIndexPredicateSQL`
"builds both" the index and the reads. That sentence overstated the guard, and it
cannot be corrected in place: the migration shipped, and
`scripts/verify-migration-immutability.sh` refuses any edit of a shipped migration; its
only standing exception is the #7002 restore of migration 093, and no exception covers
343. The exact guards are:

| Claim | Test |
| --- | --- |
| Migration 343 indexes the predicate the constant builds | `TestPreciseIndexMigrationCarriesThePredicate` |
| Every shipped read that applies the precise filter spells the constant's match reason list, in order | `TestShippedReadsSpellThePreciseMatchReasonList` |
| A finding the classifier calls precise has status `affected_exact` or `not_affected_known_fixed` and a non-empty observed version | `TestPreciseImpliesStatusAndObservedVersion` (reducer core) |
| `impact_status` and `observed_version` are parts of the canonical key, so every support of a key shares them with its winner | `TestCanonicalKeyCarriesStatusAndObservedVersion` (storage) |

The last two together are the premise of the status prune of the unscoped precise
read. A change to the precise filter needs a new index migration, not an edit of 343.
