# Evidence: #8582 dormant-manifest quota scope (fix-inline lane)

## What changed

`componentCollectorInstancesFromEnv` appends a manifest to the quota
lookup input only when the planner produced at least one instance for
it, so a dormant component contributes no budget and cannot raise a
kind conflict. `buildComponentQuotaLookup` logic and signature are
unchanged; code comments, the coordinator README, claim-quotas, and the
env registry (page plus entries mirror) now say "admitted manifests of
components planning at least one claims-enabled instance". Arbiter
fix-inline ruling recorded on #8582.

## Why no regression

No-Regression Evidence: startup-only slice-append condition on a
non-hot path. No query, worker, lease, concurrency, or graph surface
touched; the diff adds no hot-content pattern (the single flagged file,
`go/internal/envregistry/entries.go`, matches the gate's whole-file
content scan on pre-existing prose and ships a one-line description
sync only). Baseline: origin/main b214e3d54, where a dormant same-kind
quota aborts `LoadConfig` and a dormant sole declarant is served.
After: `go test ./internal/coordinator/ ./internal/envregistry/
-count=1` green, including 4 new LoadConfig-level tests that fail on
the unfixed code and pass with the fix, plus the untouched
conflict/fan-out/override builder tests. No benchmark: the lookup is
built once at coordinator startup and never on the claim path.

## Why observability is unchanged

No-Observability-Change: no new span, log key, metric, status field,
env var, or operator surface. Startup success and failure continue
through the existing `LoadConfig` error returns. The registry
description edit changes prose only.
