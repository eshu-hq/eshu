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
