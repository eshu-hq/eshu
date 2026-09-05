# #6309 review follow-ups — no-regression note for the hot-flagged files

The performance-evidence gate flags four files in this branch as hot. None of
them changes a statement the runtime dispatches or a worker/queue/lease it
runs. This note names the measurement behind each hot file and says why the
change is safe. It covers the review-follow-up commit on top
of head 225d65d; the branch's earlier hot changes (writers, handlers, ledger
rows) were proven by the live shards cited below.

## No-Regression Evidence:

What actually changed, per hot-flagged file:

- `go/internal/ifa/materializededges/materialized_edges_iam_instance_profile_role.go`
  and `materialized_edges_kubernetes_namespace_environment.go` — the offline
  rows-to-edges resolvers stamp one extra map entry (`evidence_source`, a
  compile-time constant) onto their derived edges. These resolvers run inside
  `go test` (the offline vacuity guard) and inside the Ifá coverage
  reconciliation; no service binary executes them on a request or drain path.
  The writer templates they mirror are untouched, so every Cypher statement
  the reducer dispatches is byte-identical before and after — proven, not
  asserted, by `TestIAMInstanceProfileRoleEdgeWriterUsesStaticTokenMerge` and
  the k8s writer's bound-row statement tests, which pass unchanged.
- `go/internal/reducer/iam_instance_profile_role_materialization.go` — one
  exported alias (`IAMInstanceProfileRoleEvidenceSource`) of the existing
  unexported constant. Compile-time only; the handler body, the intent flow,
  and the writer arguments are untouched.
- `go/internal/storage/cypher/materialized_edge_families.go` — comment-only
  in this commit (the registry entries it describes landed earlier in the
  branch and were proven by the live shards below).

Measured, all on darwin/arm64 go1.26.6, `cd go && go test -count=1`:

- `./internal/ifa/materializededges/` ok 1.025s (includes the RED-then-GREEN
  `TestGuardedDirectFamiliesResolveTheirOduCovered`: fails before the
  resolver change, passes after, proving the new fixture keys are
  load-bearing rather than ignored).
- `./internal/storage/cypher/` ok 1.290s (includes the new
  `TestIAMInstanceProfileRoleEdgeWriterAnnotatesLifecycleRows` pinning the
  annotated row contents).
- `./cmd/ifa/` ok 0.674s (includes the two new hermetic asserted-property
  tests: a dropped or wrong `evidence_source` fails loud).
- `./internal/reducer/` ok 3.130s. `./internal/ifa/` ok 7.745s.

Reported, not compared: the figures are recorded so a later change that makes
these packages slow has a number to regress from. Classification: diagnostic
win — no wall-clock claim is made or implied.

Live-matrix standing (unchanged runtime since): fault shards 3/4 and 4/4 on
the prior head passed with identical per-cell graph digests (k8s ec50d8 x3,
IAM 87b7a5 x3, zero dead letters) and the determinism matrix asserted 12/12
exact edge sets. Those runs executed the same writer code this commit leaves
byte-identical; this commit only strengthens what the gates assert about the
edges (one more pinned property) and how the kill cells count retries
(excess-attempt totals instead of the saturating row count). The new assertions
run in CI on this head; the hermetic verifiers
(`test-verify-ifa-fault-injection.sh`, `test-verify-ifa-determinism.sh`) pass
locally on this head and prove the shell half without Docker.

Queue-shape note for the retry-signal change, since it reads the queue: the
new `sum(attempt_count - 1)` query carries the same `stage`/`status`/`domain`
predicates over the same `fact_work_items` rows as the count query it replaces
for these two single-row domains; multi-row domains keep the count form
untouched. One row per domain means the aggregation scans one row.

## No-Observability-Change:

No metric, span, log field, or status field is added, removed, or renamed. Two
kill-cell `printf` lines reword "retry baseline" to "retry-attempt baseline"
in cell stdout logs; nothing scrapes those strings (the gate's own pins match
helper names, not this prose). The new assert helper prints the winning count
to stdout exactly like the helper it replaces. At 3 AM the operator sees the
same signals: per-cell wall-time lines, digests, dead-letter counts, and the
fault-free baseline print, all unchanged in shape.
