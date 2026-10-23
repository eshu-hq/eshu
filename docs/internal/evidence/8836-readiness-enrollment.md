# Changed-since link readiness enrollment (#8836)

The four live test files in `go/internal/reducer/freshness/links` define six test roots. The unchanged roots passed together against disposable PostgreSQL 18.6 in the pinned Bookworm image `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` on baseline `238b4015a926746a6604fb60b5b67c073a016bc2`.

The local command was `go test -json -race -p=4 -count=1 -timeout=15m ./internal/reducer/freshness/links -run '^(TestLockTimeoutsNeverCount|TestTwoProcessRunnersLinkEachActivationOnce|TestLinkAndRetentionProcessesRace|TestRunnerPoisonsAFailingLinkAfterMaxAttempts|TestNonCountingOutcomesNeverPoison|TestRunnerMovesOnPastABusySlot)$'`. The captured receipt at `/tmp/eshu-7533-drive/8836-baseline/receipt.json` records exit 0, all six run and pass events, no skip or fail event, package elapsed 40.435 seconds, and process wall 60.034 seconds. This is a single shared-host proof, not a performance benchmark.

The readiness registration selects those exact roots and requires the parent `ESHU_POSTGRES_TEST_DSN`. The two-process test supplies `ESHU_CHANGED_SINCE_LINK_CHILD_DSN` only to spawned test binaries; it is never a readiness parent requirement or optional workflow variable. The readiness runner's production event verifier requires every selected root to pass and fails on skips or missing results. Its hosted Go command omits `-race`; the local race proof supplies that additional evidence. This readiness job is path-selected, not universally required. A hosted readiness pass on the enrolled PR head is required before merge.

The same six roots also passed on enrolled commit `6aa6e9cc1d2eedf6d14492733d82de56317256bf` with `-race`, exit 0, no skip or fail events, package elapsed 37.273 seconds, and process wall 39.048 seconds. The final receipt is `/tmp/eshu-7533-drive/8836-final/receipt.json`. This second run confirms the marker-only commit; its timing is a separate observation, not a cold-cost comparison.

This enrollment changes test registration only. The six test bodies, subprocess behavior, SQL, runtime telemetry, and production code are unchanged.
