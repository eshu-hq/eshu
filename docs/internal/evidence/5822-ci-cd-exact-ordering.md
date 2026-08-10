# #5822 exact CI/CD correlation ordering

## Disposition

The runtime failure described by #5822 is fixed on `main`. PR #5846 made an
exact correlation reachable by retaining and narrowing on
`build_provenance_repository_ids`. PR #5900 then replaced unordered blanket
replay with durable producer-completion convergence. This branch does not
change production code. It makes the current exact outcome a required B-12
query contract so a future ordering regression cannot silently demote the
golden CI/CD run.

The acceptance criterion inherited from #5428 does not justify another graph
writer. PR #5824 rescinded that writer because NornicDB identifies a
relationship by `(start, end, type)`: two evidence owners writing the same
`BUILT_FROM` edge can overwrite or retract each other's truth. Container image
identity remains the sole graph writer. CI/CD correlation outcome remains
Postgres-backed read truth, while #5827 owns the broader shared-edge identity
class.

## Ordering and convergence

The failing historical interleaving was:

1. CI/CD correlation ran before container image identity and persisted a
   non-exact outcome.
2. Identity later published the missing exact-digest evidence.
3. An unordered replay could finish CI/CD correlation too early again, leaving
   the derived result durable.

The current completion path records producer completion atomically with the
identity work acknowledgement. Fanout reopens a succeeded consumer, or marks a
running consumer for replay so a stale acknowledgement is rewritten to
pending. The completion event is removed only after that handoff. This gives
the consumer another evaluation after the producer's evidence is durable,
without reducing worker concurrency or introducing a second graph owner.

The operator-facing contract is unchanged. Existing completion-queue depth and
age metrics expose stalled convergence, structured fanout logs report success
or failure, and CI/CD correlation outcome counters expose exact, derived, and
ambiguous decisions.

Root-Cause Evidence: the retained #5426 gate database contained 16 identity
rows for the artifact digest with no `build_provenance_repository_ids`; the
correlation was `ambiguous`, `provenance_only=true`, and became exact after the
CI build provenance survived publication. Separately, the retained #5740 gate
database had one pending identity-completion event while the two older work
ledgers were empty; the old drain returned success, then the same reducer
consumed the event and reopened the CI/CD and supply-chain consumers. Those
observations establish both the missing exact-match input and the false
completion boundary that let non-exact cross-scope truth remain visible.

## Current-main baseline

A retained B-7 database on base commit
`867d96f64e26bd7f0e62b7b6fccafc9da406114a` contained one exact, one derived,
and one ambiguous CI/CD correlation. The exact row was:

```text
scope_id:          ci_cd_run:github_actions:eshu-hq:supply-chain-demo
provider:          github_actions
run_id:            5150
repository_id:     repository:r_69256c06
artifact_digest:   sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890
outcome:           exact
canonical_writes:  1
reason:            exact: one container image identity row matched the artifact digest
```

All three correlation work items were succeeded and the completion-event queue
was empty. The unmodified current-main gate passed with `521 pass, 0
required-fail, 1 advisory-warn` in 123 seconds.

## B-12 regression guard

The existing production-environment HTTP assertion now calls:

```text
GET /api/v0/ci-cd/run-correlations?environment=prod&limit=10&outcome=exact&provider=github_actions&repository_id=repository:r_69256c06&run_id=5150
```

It requires at least one result. Provider and run filters isolate the authored
run, while one `required_json_object_matches` entry binds the digest,
repository, provider, run, environment evidence, exact outcome, and one
canonical write to the same returned object. Independent JSON-path matches are
not sufficient here because different rows could satisfy different values.
Filtering the request itself on `outcome=exact` prevents a derived or ambiguous
run from satisfying the assertion merely because its other fields still match.

## BITES proof

Two hostile mutations were tested and then removed:

- Demoting the real single-match classifier from exact to derived made the
  full gate fail earlier in downstream supply-chain suppression and
  environment-evidence checks. That proved the dependency, but not the new
  assertion's independent teeth.
- Forcing only the HTTP query handler's outcome filter to `derived` let all
  earlier checks pass and failed at the new route with
  `"correlations" has 0 results, want >= 1`. The run reported `520 pass, 1
  required-fail, 1 advisory-warn` and exited 1 after 119 seconds. This final
  mutation used the provider/run-isolated, object-bound assertion after review
  closed the independent-path false-green.

Both production files were restored byte-for-byte before the clean run. Their
diffs were empty; only the intended snapshot and evidence changes remained.
The same full command then passed:

```text
scripts/verify-golden-corpus-gate.sh
summary: 521 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 115s, budget ceiling 1800s)
```

The exact run-5150 route returned one correlation and passed its correlated
object match. The advisory was maintenance-drain timing: 27 seconds observed
against a 19-second advisory ceiling. Pipeline wall time, all required queue
states, and all HTTP, MCP, graph, and demo assertions passed.

## Performance and observability

No-Regression Evidence: this branch changes only a committed B-12 assertion
and its evidence record. It does not change SQL, reducer decisions, queue or
lease behavior, graph writes, worker counts, API implementation, or runtime
settings. The clean full gate completed in 115 seconds, 8 seconds faster than
the unmodified current-main baseline on the same machine and corpus. This is
verification context, not a performance-improvement claim.

No-Observability-Change: no runtime instrument, label, span, log field, or
status surface changes. Operators retain the current completion-queue depth
and age metrics, structured fanout logs, and CI/CD outcome counters described
above.
