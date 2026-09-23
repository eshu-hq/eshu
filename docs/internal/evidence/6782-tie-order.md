# Evidence: tie-order disposition (entry-44 flap)

## Problem

Allowlist entry 44 (the `file_count`-by-language read, `ORDER BY` over
tied keys) lived in `entries`, where the stale check fails the gate on
exactly the runs where both backends deliver the tied rows in the same
order. Observed: gate red on main `9bdc15b8c` (`entry 44 ... matched no
divergence in this run (stale)`), green 23 minutes later on `a07ee93bf`
with no intervening oracle or spec change; same message on PR #6985.

## Change

New `tie_order_reads` allowlist section (branch
`gate/6782-tie-order`): fingerprint-keyed, reason plus upstream-issue
accountability, no tier, never stale-checked. The parse guard requires
`ORDER BY` (reusing `backendconformance.HasOrderBy`, the comparator's
own predicate) and rejects `LIMIT`/`SKIP` via a literal/comment-aware
whole-word scan — with truncation, tied keys change which rows return.
Exclusion covers only the `results` kind and reports in the new
`nornicdb_vs_neo4j_tie_order` advisory finding, counted toward the
advisory ceiling. Entry 44 moved there. The pairing dump header now
reads `pre-advisory-split` with the transient/tie counts partitioned
out, so triage stops reading it as the failure.

## Before/after

- Parse: committed spec parses as entries=46/transient=2/tie-order=1
  (was 49 entries incl. entry 44 on the pre-#6991 base; #6991 retired
  two #6915 entries on main in the meantime).
- Seeded RED/GREEN unit pairs: 9 capture tests (guard boundaries,
  kind scope, never-stale), quorum tie advisory, tie ceiling
  contribution — all RED before, GREEN after.
- Focused suites: `graph/capture`, `golden-corpus-gate`,
  `backendconformance` green; gofumpt/vet clean.

## Timing

Shared offline-phase test
`TestRunBackendDiffQuorumPairingReportIsBounded` (`-count=3`), same
host back-to-back:

- base (`origin/main 30a29bade`): 3/3 PASS
- branch (`gate/6782-tie-order`, after rebase): 3/3 PASS

The change adds one linear pass over the already-materialized
per-pairing diff list plus one advisory finding line. The per-statement
hot recording path (`decorate.go` / sink `Append`) is untouched.

No-Regression Evidence: the tie-order code runs only inside the gate's
offline backend-diff phase (parse once at startup, exclusion over
in-memory diffs). The shared quorum test passes 3/3 on both sides with
no measurable delta; verdicts differ only as designed (tie divergences
report advisory instead of failing or stale-failing).

No-Observability-Change: one additive finding name
`nornicdb_vs_neo4j_tie_order` (non-required WARN) plus the reworded
pairing header. No metric, span, log key, or status field changes;
existing finding and flag names unchanged.
