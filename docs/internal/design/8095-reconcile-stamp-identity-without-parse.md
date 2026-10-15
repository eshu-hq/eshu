# #8095: stamp the pre-parse identity without a full parse (design note)

The first reconcile sweep after the #7978 fix parses every repository once
only to store a pre-parse identity. With `ESHU_REPO_RECONCILE_MAX_PER_CYCLE=10`
and ~6-minute cycles, 809 repositories take ~81 cycles (~8 hours), plus one
65–80 s deferred maintenance run per batch. The repositories are already
fetched and their commits unchanged; the parse exists only to write the
identity.

## Proof

A stamp is safe only when the active generation provably equals what a full
parse of the same commit under the running collector would emit. That proof
has three parts, all of which must match:

1. **Same source commit.** The fetched remote head equals the active
   generation's source commit (already checked: `head_moved` parses).
2. **Same content version.** The active generation was produced by the running
   `GenerationContentVersion()` (collector + parser-registry fingerprint +
   fact-schema digest).
3. **Same configuration digest.** The active generation was produced under the
   running `preparse.Config` (ignore/preserve globs, dataflow gate, SCIP
   set and binaries, dependency mode, GitLab host), with **no degraded
   snapshot** (SCIP fallback, unreadable file: those generations carry no
   identity today and must keep parsing).

Parts 2 and 3 are not recorded anywhere: a stored generation carries a
freshness hint and a source commit, but neither the content version nor the
configuration digest that produced it. So **no pre-fix generation can be
proven**, and the issue does not claim otherwise ("Whether the identity can be
derived for old generations at all" is explicitly not checked).

## Decision

Ship the gate fail-closed in two parts:

- **Part 1 (this change).** The opt-in setting
  `ESHU_REPO_RECONCILE_STAMP_IDENTITY_WITHOUT_PARSE` (default off), the
  `stamped_without_parse` outcome, and the `Decide`/`Run` branch that stamps
  only when the setting is on **and** the proof matches. The collector passes
  "unproven" until the store records the proof, so the setting parses as
  before today: no change to default behavior.
- **Part 2 (follow-up).** Record the content version and configuration digest
  on every parsed generation (payload or column), expose them through
  `FullReconcileState`, and pass "proven" when both equal the running values
  and the generation is not degraded. Only then can the setting stamp, and
  only for generations parsed after part 2: the first sweep after part 2
  still parses each old generation once (recording its proof), and later
  sweeps skip.

A re-read of the stored facts is therefore **not** enough to confirm the
identity: only the recorded version + digest prove a re-parse would emit the
same facts. A re-parse is the only confirmation until part 2 records them.

## Failure modes

- Proof absent or mismatched (old generation, version bump, config change,
  degraded snapshot): parse as today (`identity_absent` / `identity_mismatch`).
- The stamp reuses the fenced re-open machinery through a dedicated store
  stamp statement (`stampActiveGenerationQuery`): the fence (active pointer,
  full generation, commit, empty stored identity, non-empty request) still
  refuses a stale stamp with `reprojection_superseded`, which parses in the
  same cycle. The stamp never takes the identity-equality path, whose fence
  would refuse every stamp. A second stamp after the Ack finds a stored
  identity and parses (one-shot by construction).
- The stamp re-open stamps `preparse_skips` 0, so a stamp counts as a parse
  for `ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS`; a due valve still forces a real
  parse. A stamp still spends an `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` slot.
- Submodules, dirty tree, moved head, delta-active, and ineligible reasons
  (reindex, stale request) always parse; the stamp never widens the
  `interval_elapsed` / `graph_dirty` set (ruling a9-143 D1).

## Open questions for the arbiter

1. Is content-version + configuration-digest equality (plus non-degraded) the
   accepted proof, or must the stamp also re-read stored facts?
2. Should part 2 backfill proof for old generations from any other record, or
   is "parse once after part 2" the accepted migration?
3. Does a stamp answer a reindex watermark, or must an explicit reindex always
   parse (ruling a9-125 suggests parse; the gate keeps `reason_ineligible`)?

Until the arbiter rules, the gate stays fail-closed and the answers above are
the implementation's standing assumptions, not rulings.

## No-Regression Evidence:

- Baseline: `origin/main` at `74d6777a81` — every `identity_absent` parses.
- After: this PR's head — with the setting off or unproven, the sweep takes
  the identical path plus short-circuit boolean checks in `Decide`; with a
  proven stamp, `Run` takes the store stamp statement instead of the
  identity-equality path (same lock, same re-open guard, one extra fenced
  predicate on the empty stored identity). The exact head SHA and the full
  benchmark table are in the PR body; the numbers below were re-measured on
  the final tree (no stale head).
- Benchmark Evidence: in-tree `BenchmarkDecide`
  (`go/internal/collector/repo/git/preparse/decision_bench_test.go`),
  1e8 iterations x3, AMD EPYC 9R14, linux/amd64, go1.26.9: eligible-skip
  12.0 ns/op, stampable 11.8 ns/op, identical within noise — about six orders
  of magnitude below the guarded `git status` subprocess (milliseconds) and
  a full parse (minutes).
- Input shape: synthetic eligible and stampable `Input` at unit level; no
  corpus, profile, or topology change. The stamp statement touches the same
  rows as the skip-path re-open (one generation row, its canonical projector
  row), so terminal queue and row counts are unchanged by construction.
- Why safe: no new I/O, locks, goroutines, or allocations on the decision
  path; the change is fail-closed, so production takes the old path until
  part 2 records the proof.

## Observability Evidence:

- The existing `eshu_dp_collector_reconcile_parse_skip_decisions_total`
  counter gains the `stamped_without_parse` label value (closed set 14 to 15);
  no new instrument. The span event `git.reconcile.parse_skip_decision` and
  the INFO `git_reconcile_parse_skipped` log are reused, with the outcome
  asserted in `TestRunStampsIdentityWithoutParsing`.
- An operator counts stamped repositories against parsed ones with
  `eshu_dp_collector_reconcile_parse_skip_decisions_total{outcome="stamped_without_parse"}`.
