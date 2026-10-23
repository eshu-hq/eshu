# #8834: quota proof readiness enrollment

The six existing quota claim proofs passed on a disposable PostgreSQL 18.6 service before enrollment. All six exact top-level tests emitted `run` and `pass` events under `-race -count=1`; the package passed, with no skip or failure event. This supports registering their three files in `live-postgres-readiness`. It does not establish a hosted CI result.

## Proof identity and cost

- Source commit: `238b4015a926746a6604fb60b5b67c073a016bc2`.
- Image: `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c`, the readiness workflow's pinned image.
- Toolchain: Go 1.26.10 on linux/amd64.
- Command from the `go/` directory: `go test -json -race -p=4 -count=1 -timeout=15m ./internal/storage/postgres -run '^(TestClaimQuotaOverBudgetThrottlesOnIdleSystem|TestClaimQuotaRacingReplicasCannotOvershoot|TestClaimQuotaStateSurvivesCoordinatorRestart|TestClaimQuotaUnreadableFallsBackToFloorRate|TestClaimQuotaGatedPlanStaysIndexServed|TestClaimQuotaScopesPartitionBudget)$'`.
- Go package terminal elapsed: 13.557 seconds. The second attempt's process wall time was 139.431 seconds, including cold compilation. The first cold-compilation attempt was stopped before any test event to increase build parallelism. These values describe enrollment cost; they are not a controlled before/after performance comparison.
- Raw local receipt and JSON events: `/tmp/eshu-7533-drive/8834-baseline/receipt.json` and `/tmp/eshu-7533-drive/8834-baseline/events.jsonl`. These paths are local artifacts, not committed hosted evidence.

## Enrollment scope

The change adds runner metadata to three existing test files, replaces their scheduled ledger rows with generated `postgres_ci` rows, and shrinks the frozen legacy exemption inventory. It changes no test body, SQL, assertion, production runtime, or operator telemetry. The readiness runner's hosted acceptance on the final PR head remains pending.
