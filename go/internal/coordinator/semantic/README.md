# Semantic-provider execution worker

## Purpose

`semantic` runs the egress-gated semantic-provider execution worker. It claims
queued semantic extraction jobs, re-checks the semantic egress policy at claim
time (fail closed), and terminates or dispatches each job through an
explicitly enabled provider client.

## Ownership boundary

This package owns the worker execution loop (`ProviderWorker.Run`),
environment-driven configuration loading (`LoadProviderWorkerConfig`), and
worker-scoped OTEL metrics (`NewProviderWorkerMetrics`). The parent
`internal/coordinator` package owns `Service` wiring (the
`SemanticProviderWorker` field), the maintenance pass that invokes `Run`, and
deployment-mode and claims-enabled gating. This package declares its own
`GovernanceAuditAppender` interface rather than importing `internal/coordinator`,
and `NewProviderWorkerMetrics` takes the instrument prefix as an argument
(callers pass `coordinator.MetricPrefix`) so it never imports the coordinator
root.

Exported names were de-stuttered on the #6781 Part A move:
`coordinator.SemanticProviderWorker` is now `semantic.ProviderWorker`,
`DisabledSemanticProviderClient` is `DisabledProviderClient`, and
`LoadSemanticProviderWorkerConfig` is `LoadProviderWorkerConfig`. The
`ESHU_SEMANTIC_PROVIDER_*` environment variable strings are unchanged.

## Exported surface

- `ProviderWorker` is the worker; `Run` drains configured scopes once per call.
- `ProviderWorkerConfig` and `LoadProviderWorkerConfig` load the worker
  configuration from environment.
- `ProviderClient`, `DispatchRequest`, `DispatchResult`,
  `DisabledProviderClient`, and `ErrProviderExecutionNotEnabled` define the
  provider dispatch contract and its no-network default.
- `ExtractionClaimer` is the narrow durable claim surface the worker needs.
- `GovernanceAuditAppender` is this package's own audit-append interface.
- `ProviderWorkerMetrics`, `NewProviderWorkerMetrics`, and
  `ProviderClaimObservation` cover claim telemetry.
- `Env*` constants name the `ESHU_SEMANTIC_PROVIDER_*` environment variables.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/environment` for typed environment parsing (`Bool`,
  `Duration`, `Int`).
- `internal/semanticpolicy` for the egress policy and `EvaluateEgress`.
- `internal/semanticqueue` for the durable claim record, failure, and budget
  types.
- `internal/governanceaudit` for the audit event shape.
- `internal/coordinator/governance/audit` for the shared service ID, hashing,
  and correlation-ID helpers.
- `internal/telemetry` for the shared metric dimension names.

The package does not import the parent coordinator package.

## Telemetry

`NewProviderWorkerMetrics` registers one OTEL counter,
`<prefix>semantic_provider_claim_total`, incremented once per claim outcome.
Its label dimensions are `outcome` (`egress_denied`, `egress_policy_missing`,
`provider_disabled`, `dispatched`, or a dead-letter failure class),
`provider_kind`, `provider_profile_class`, and `source_class`. All four are
redacted, low-cardinality labels — `redactedLabel` maps an empty value to
`"unknown"` — and never carry a provider host, endpoint, URL, credential,
prompt, or response body. An operator watching this counter can tell whether
the worker is denying on egress, running fully disabled (the default), or
actually dispatching, broken down by provider and source, without the metric
itself revealing anything about a specific job's content.

No-Observability-Change: this move renames no metric, span, log field, status
field, queue, worker, lease, or runtime setting. The claim counter existed
under the coordinator package before the split.

## Gotchas / invariants

- Both `ExecutionEnabled` and a client whose `Enabled()` returns true are
  required before `Dispatch` is ever called; the egress gate runs before that
  check regardless of either flag.
- The default `DisabledProviderClient` performs no network I/O and always
  returns `ErrProviderExecutionNotEnabled`.
- `GovernanceAuditAppender` is declared locally, not imported from the
  coordinator root; the coordinator root's audit store satisfies it
  structurally.
- `NewProviderWorkerMetrics` requires its caller to supply the coordinator's
  metric prefix; it has no default of its own.
- `Run` is a no-op when the worker is disabled or no `Claimer` is configured,
  so it is safe to wire unconditionally into a scheduling loop.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/semanticpolicy/README.md`
- `go/internal/semanticqueue/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`

## Runtime contract and evidence (moved from the coordinator root, #6781)

`semantic.ProviderWorker` is the egress-gated semantic-provider execution
worker (`semantic/provider_worker.go`), held by the optional
`Service.SemanticProviderWorker` field. It claims semantic extraction jobs,
re-checks
semantic egress with `semanticpolicy.EvaluateEgress` before consulting a
provider client, and runs from `runActiveMaintenance` only when active-mode
claims and the worker are enabled.

The worker ships no provider traffic by default. `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED`
turns the claim loop on; `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` and a
concrete enabled provider client are also required before dispatch. The default
`semantic.DisabledProviderClient` performs no network I/O and terminates allowed
claims as `provider_execution_not_enabled`. Denied or missing egress policy
skips the claim behind the lease fence and records a redacted governance audit
event with low-cardinality reason data only.

Performance Evidence: `BenchmarkSemanticWorkerEgressGatedClaimLoop` measures the
full gated claim cycle with the default no-network client. Baseline on Apple
M4 Pro, Go test harness, single pending documentation job per iteration:
~953 ns/op, 2200 B/op, 14 allocs/op. After is identical to baseline because the
default client performs no network or queue I/O beyond the in-memory egress gate
and a single lifecycle write; terminal queue disposition is exactly one row per
claim (skipped_policy on deny, dead_letter/provider-disabled on allow). The
claim loop is lease-fenced and bounded by `MaxClaimsPerPass` per scope, so a
single scope cannot starve the loop. No serialization workaround was introduced:
the postgres `ClaimNext` query uses `FOR UPDATE SKIP LOCKED` so concurrent
workers claim disjoint rows.

No-Regression Evidence: `go test ./internal/coordinator/semantic -run 'TestSemanticWorker|TestLoadSemanticProviderWorkerConfig' -race -count=1`
proves denied-egress fail-closed skip, missing-egress-policy fail-closed skip,
allowed-egress + default-disabled-client no-network termination, the disabled
client never dispatching even when the execution flag is on, allowed-egress +
enabled test client dispatching only after the gate, default-OFF worker no-op,
config defaults-off parsing, and a race-tested concurrent claim loop that
processes each job exactly once. Storage proof:
`go test ./internal/storage/postgres -run 'TestSemanticExtractionQueueStore(Claim|SkipByPolicy)' -count=1`
proves the lease-fenced claim returns provider profile/source class for the
egress re-check and the policy-skip transition is terminal behind the fence.

Observability Evidence: the worker emits the
`eshu_dp_workflow_coordinator_semantic_provider_claim_total` counter dimensioned
by bounded `outcome` (`egress_denied`, `egress_policy_missing`,
`provider_disabled`, `dispatched`, `provider_unavailable`), `provider_kind`,
`provider_profile_class`, and `source_class`; redacted structured logs
distinguishing the egress-skip and provider-disabled outcomes; and the redacted
`EventTypeSemanticPolicyDecision` governance audit event for every egress
decision. No provider host, endpoint, URL, credential, raw prompt, or raw
response appears in any metric label, log field, or audit field.
