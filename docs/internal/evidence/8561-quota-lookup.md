# Quota sourcing into coordinator store lookup (#8561)

## Contract

`run()` in `go/cmd/workflow-coordinator/main.go` passes a non-nil
manifest/override lookup into the store, so `Spec.Quotas` reaches the gated
claim path from production construction. New
`go/internal/coordinator/quota_lookup.go` builds the lookup from admitted
manifests plus two operator overrides; `config.go` and
`component_activation_config.go` carry the declarations. The
`go/internal/envregistry/entries.go` touch is two static `Entry` literals
(`ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR`,
`ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION`) — registry metadata
evaluated once at startup, no call-site change. There is no SQL, schema,
Cypher, queue, lease, batch, worker-count, or retry change anywhere in the
commit.

## Performance and observability evidence

The `perf-evidence` gate selects this change because `entries.go` sits under
its hot-file location. The touch is startup-only metadata, so this note
records no-regression plus observability rather than a benchmark shaped like
proof.

No-Regression Evidence: the two added entries are inert data in the
`coreEntries` slice. They add no branch, loop, I/O, lock, goroutine,
channel, SQL, or Cypher to any steady-state path; every success and
steady-state path compiles and runs the same instructions as before. Startup
does one registry pass reusing the same readback (no second grant re-read;
rationale in code comment). Input shape is blank-or-integer env vars,
exercised by `quota_lookup_test.go` (366 lines: override-wins,
fail-closed on 6 invalid-override cases, kind-conflict rejection) plus the
new production-path wiring test tracing env install to `LoadConfig` to
`run()`'s exact store call. `go test ./internal/coordinator/
./cmd/workflow-coordinator/ -count=1` ok (both packages); the
`Quota|Wiring|ProductionWiring` subset passes including under `-race`;
`go vet` on both packages clean. No live backend is a variable here because
no backend is touched; a before/after latency measurement would be measuring
identical instructions on the steady-state path, so none is recorded. Live
quota proof (overshoot, restart survival, unreadable-table floor) already
exists in #8557's live tests and re-runs in the CI wave post-push.

Observability Evidence: this commit adds no metric, span, log line, status
field, or telemetry label. The quota-throttle signal it feeds
(`recordQuotaThrottle` on the gated path) pre-exists from merged #8557; the
depth gauge it complements ships on the sibling #8472 branch. Operator
visibility for the new knobs is documentation: `claim-quotas.md`,
`env-registry.md`, and both READMEs updated in this commit. No production
signal is added, removed, or renamed.

Why it is safe: registry-metadata-only hot-file touch; startup-fail-closed
on misconfiguration; concurrent claim reads over an immutable map;
fail-closed paths pinned by unit tests; package suites, race subset, and vet
green.
