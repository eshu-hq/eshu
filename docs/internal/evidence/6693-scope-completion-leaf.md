# #6693 scope/completion/ leaf

Change: checklist step 22 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the four cross-scope completion production files out of root
`internal/storage/postgres` into
`go/internal/storage/postgres/scope/completion`, nested under the `scope/`
leaf. The completion queue lets producer domains publish durable completion
events, the reducer claim and fan them out to eligible consumers under a
lease, and consumers defer through the producer-readiness floor until every
producer domain they read reports ready.

Package clause: `completionstore` -- plain directory word plus `store`, with no
pre-existing `^package completionstore$` collision in `go/`. Callers import the
`storage/postgres/scope/completion` path without an alias (the path's last
element differs from the package name, so no alias is needed and none is used).

Files moved (byte-identical apart from the package clause, except for the
export below): `cross_scope_completion_fanout.go` -> `scope/completion/fanout.go`,
`cross_scope_completion_queue.go` -> `scope/completion/queue.go`,
`cross_scope_producer_readiness.go` -> `scope/completion/producer_readiness.go`,
`scope_quiescence.go` -> `scope/completion/quiescence.go`. The move drops the
`scope_quiescence.go` dirgate marker the checklist names. Three self-contained
tests move with them: `cross_scope_producer_readiness_test.go`,
`scope_quiescence_test.go`, `scope_quiescence_live_test.go`.

Membership correction the mapping did not cover (root cause, not
improvisation): three mapped tests stay in root past this step and move with
their owning families instead --

- `cross_scope_completion_concurrency_postgres_live_test.go` and
  `cross_scope_completion_snapshot_postgres_live_test.go` open their database
  only through `openContainerImageIdentityAckCapabilityProofDB`, whose chain
  (`openActiveOCIWarningIndexProofDB`, `activeOCIWarningIndexSchemaDSN`, the
  production `quoteSQLIdentifier`, `ApplyBootstrap`) is owned by the
  container/image family and root. Moving them now would duplicate that
  harness plus production identifier-quoting logic into completion tests;
  duplicating shared production logic across packages lets the copies drift,
  so they stay until `container/image/` (step 25) owns the harness move.
- `reducer_queue_ack_scale_plan_test.go` constructs `ReducerQueue` with its
  unexported `database` field and drives `AckBatch`, and uses five helpers
  owned by staying files; it moves with `queue/reducer/`, which owns
  `reducer_queue.go`.

The mapping in `ingestion.md` records the deferral, the two receiving
sections (`container/image/`, `queue/reducer/`) gain the relocated lines with
`#` annotations, and the tally stays net-zero. The plan's own SPLIT precedent
(`reducer_queue_ack_fanout_plan_test.go`, which reads privates of two future
leaves and stays in root) is the same shape.

Production export for staying callers (no duplication): the staying root plan
tests EXPLAIN the shipped fanout query, so `fanoutCrossScopeCompletionQuery`
became `FanoutCrossScopeCompletionQuery` in `scope/completion/fanout.go` with
a comment saying why. Everything else the stayers need was already exported.
The staying concurrency proof reads the store clock through the exported
`Now` field instead of the unexported `now` method; the field is set
immediately before both reads to a UTC clock, so the two forms agree.

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/reducer/main.go`, `cmd/reducer/wiring_handlers.go`: store
  construction repointed to `completionstore.*` (fields stay unexported;
  both structs are still built as literals with `DB:`).
- Thirteen staying root tests plus the fanout plan test: `completionstore.`
  qualifier on `NewCrossScopeCompletionStore`,
  `CrossScopeProducerReadinessStore`, `ErrCrossScopeCompletionClaimRejected`,
  and `FanoutCrossScopeCompletionQuery`.

Test form: the three moved tests stay in-package (`package completionstore`,
self-contained). The moved readiness test swaps its `fakeRows` double for the
shared `fake.Rows` (same row shape, same string/bool scan targets) instead of
copying the root fake. The moved quiescence live test keeps twin copies of
`seedAWSCloudRuntimeDriftGeneration` and `awsCloudRuntimeDriftAdmissionLiveDB`
(root's `aws_cloud_runtime_drift_admission_live_helpers_test.go` defines the
originals for the staying drift proofs; Go test-only symbols do not cross
package boundaries), each with a pointer comment naming its twin.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on the
lock leaf. Live citations repointed: `go/internal/reducer/crossscope`
(comment and README), the projector container-image `AGENTS.md` (which
requires the citation to stay in sync), and `specs/live-tests.v1.yaml` (the
moved quiescence live file).

Adding the `scope/completion/` directory drops the root file count; dirgate's
grandfather row re-pins down accordingly, with `grandfather.go` regenerated.

Verification: `go build ./...` clean; `go vet` clean on the leaf, root, and
`cmd/reducer`; `gofumpt -l` clean; `git diff --check` clean;
`scripts/verify-dirgate.sh` clean; `scripts/verify-moved-file-refs.sh`
reports only the dated-evidence pointers the allowlist decision covers.
`go test ./internal/storage/postgres/scope/completion/ -list '.*'` lists all
moved tests, and the full `internal/storage/postgres/...` suite passes with
`-race` (live proofs skip without a DSN locally and run in CI).

No-Regression Evidence: focused package tests and every repointed caller suite pass on the moved tree, including the moved quiescence live proof and the staying concurrency, snapshot, and plan proofs against the exported surface; the completion SQL text is unchanged so no plan or lifecycle proof is re-owed.
No-Observability-Change: package move only, with no metric, span, log field, worker, queue, lease, retry, or runtime-knob edit anywhere in the diff.
