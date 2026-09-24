# #6693 queue/ leaf

Change: checklist step 15 of `docs/internal/design/6693-postgres-target-tree.md`.
It adds the `go/internal/storage/postgres/queue` leaf (`package queuestore`)
with the `queue/` section's two non-test files plus their mapped test:

- `failure_metadata.go` -> `queue/failure_metadata.go`
- `retry_backoff.go` -> `queue/backoff.go`
- `retry_backoff_test.go` -> `queue/backoff_test.go`

Exported (unqualified in root, package-private in the new leaf otherwise):
`queueFailureMetadata` -> `queuestore.QueueFailureMetadata`,
`deadLetterTriageMetadata` -> `queuestore.DeadLetterTriageMetadata`,
`computeRetryDelay` -> `queuestore.ComputeRetryDelay`,
`defaultRetryMaxDelayFallback` -> `queuestore.DefaultRetryMaxDelayFallback`,
`defaultJitterSource` -> `queuestore.DefaultJitterSource`. `maxBackoffShift` and
`newSeededJitterSource` stay unexported; both are used only inside the new
package (`backoff.go` and its in-package `backoff_test.go`).

Prerequisite hoist (from the top-level plan's "Prerequisite hoists" table):
`sanitizeFailureText` moved from `projector_queue.go` (root, stays in root
until the later `queue/projector/` move) into `failure_metadata.go` as an
unexported `queuestore` helper — its only caller, `queueFailureMetadata`, is
what moved. Root's `projector_queue.go` no longer defines it; its `strings`
import stays (used elsewhere in that file). The plan's second `queue/` hoist
row, `claimedAtValue` (from `reducer_queue_helpers.go`), was left in root: it
is not used by either mapped file, so this step does not need it to build,
and the plan itself allows a hoist as "the first commit of the move that
needs it" — that is the later `code/flow/` or `queue/reducer/` step.

Callers repointed (all still in root, since `ProjectorQueue`/`ReducerQueue`
move in later `queue/projector/`/`queue/reducer/` steps): `projector_queue.go`,
`reducer_queue_helpers.go`, `reducer_queue.go` (doc comments only), and four
root readiness tests (`reducer_queue_gcp_relationship_readiness_test.go`,
`reducer_queue_ec2_instance_identity_readiness_test.go`,
`reducer_queue_secrets_iam_readiness_test.go`,
`reducer_queue_kubernetes_correlation_readiness_test.go`) that asserted
`defaultRetryMaxDelayFallback`'s clamp value directly. Outside
`internal/storage/postgres`, three stray doc-comment references to the old
lowercase names were repointed to `queuestore.*` (no logic touched):
`internal/telemetry/instruments.go` (the `ProjectorRetrySurge` metric doc),
`internal/runtime/retry_policy_test.go`, and
`internal/reducer/factdecode/decode_error.go`. `rg` for every old symbol name
(`queueFailureMetadata`, `deadLetterTriageMetadata`, `computeRetryDelay`,
`defaultJitterSource`, `defaultRetryMaxDelayFallback`, `sanitizeFailureText`)
across `go/internal` finds nothing outside `queue/`.

Adding the `queue/` directory makes dirgate's sibling-name rule flag root's
`queue_observer.go`, whose mapped destination is `queue/reducer/observer.go`
(checklist step 38). That file now carries a `//nolint:dirgate` marker naming
step 38 until that move lands.

`scripts/verify-moved-file-refs.sh` also caught non-doc references to the two
vacated root paths: `.github/workflows/ifa-determinism-gate.yml`'s and
`specs/ci-gates.v1.yaml`'s `ifa-determinism-gate`/`ifa-live-gate` trigger path
lists, and `scripts/lib/ifa_live_gate_selector_cases.sh`'s selector fixture
table. All three are repointed to `queue/failure_metadata.go`/`queue/backoff.go`;
`bash scripts/verify-ci-gates-registry.sh --drift` and both YAML files parsing
via `python3 -c "import yaml; yaml.safe_load(open(...))"` confirm the registry
and workflow stayed in sync.

No-Regression Evidence: `QueueFailureMetadata`, `DeadLetterTriageMetadata`,
`ComputeRetryDelay`, `DefaultRetryMaxDelayFallback`, `DefaultJitterSource`,
and `sanitizeFailureText` are unchanged apart from the package clause and the
exported names — no SQL text, precedence, jitter formula, or overflow-guard
logic changed. From `go/` after the final edit: `gofumpt -l -w` on every
touched file printed nothing to fix; `go build ./...` exit=0; `go vet ./...`
exit=0; `go vet -tags "integration perf5854_ack perf5740_completion
perf6785_wait" ./internal/storage/postgres/...` exit=0; `go test
./internal/storage/postgres/... -race -count=1` exit=0 (includes the new
`queue` package and the four readiness tests that now call
`queuestore.DefaultRetryMaxDelayFallback`); `go test ./internal/telemetry/...
./internal/runtime/... ./internal/reducer/... -count=1` exit=0 (covers the
three stray-comment files). Root's non-test file count dropped by 2 (`bash scripts/verify-dirgate.sh --digest internal/storage/postgres`);
the `dirgate-grandfather.tsv` row and generated `grandfather.go` were
refreshed to that count and digest. `bash scripts/verify-dirgate.sh --all`,
`bash scripts/verify-moved-file-refs.sh`, and `bash scripts/verify-doc-citations.sh`
all exit=0. `go test ./internal/query -run QueryPlan -count=1` exit=0 (no
pinned source hash names either moved file).

Test-repoint proof: `go test ./internal/storage/postgres/queue/... -list
"^(TestComputeRetryDelayAppliesExponentialBackoff|TestComputeRetryDelayCapsAtMaxDelay|TestComputeRetryDelayCapsAtMaxDelayWithoutOverflowingLargeAttemptCounts|TestComputeRetryDelayAddsBoundedJitter|TestComputeRetryDelayNegativeAttemptTreatedAsZero|TestRetrySurgeSpreadsVisibleAtAcrossManySimultaneousFailures)\$"
-count=1` prints all six names and exits 0; the identical `-list` pattern
against `./internal/storage/postgres` prints none of them and exits 0 with no
match, so the tests moved rather than being duplicated or dropped. `go test
-list '.*' ./internal/storage/postgres/queue/ -count=1` prints the same six
names, confirming nothing else landed in the new package.

No-Observability-Change: every moved/hoisted function is a pure computation
(failure-cause reconciliation or backoff-delay math) with no metric, span, or
log of its own. `ProjectorQueue`/`ReducerQueue` in root keep emitting the
same `ProjectorRetrySurge`/`ReducerRetrySurge` counters and
`AttrFailureClass` attribute around the calls; no metric, span, log key, or
status field is added, removed, or renamed.
