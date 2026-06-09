# Collector Extension Host

`extensionhost` adapts public `collector-sdk/v1alpha1` extension results into
the core `collector.ClaimedService` path.

The package owns only the intake boundary:

- build a bounded JSON request from a durable `workflow.WorkItem`
- launch a host-provided runner such as `ProcessRunner`
- validate the result with `sdk/go/collector`
- compare the returned claim identity with the host claim
- map accepted SDK facts into internal `facts.Envelope` values
- classify retryable, terminal, invalid-result, and identity-mismatch outcomes

It does not receive or pass Postgres, graph, reducer, API, or MCP handles to an
extension. Claim mutation, stale-fence handling, retry budgets, and final commit
remain owned by `collector.ClaimedService` and its claim-aware committer.

No-Regression Evidence: `go test ./internal/collector/extensionhost -count=1`
proves the host passes only a bounded JSON claim/config/contract document,
validates SDK output before fact mapping, deduplicates exact SDK facts, maps
partial and unchanged states without direct graph writes, and returns
retryable/terminal classified errors for existing workflow claim mutations.

Collector Performance Evidence: `go test ./internal/collector/extensionhost
-count=1` exercises the local host path without live network, database, graph,
or reducer handles. The process runner enforces bounded stdout and stderr, the
source performs one SDK validation pass per claimed result, and exact duplicate
facts are collapsed before commit handoff so retry delivery does not amplify
same-generation fact streams.

Collector Observability Evidence: the same focused test proves SDK status
records are copied into bounded `StatusRecord` values and every final state
continues through `collector.ClaimedService` failure classes, claim rows, and
fact commit counters. Operators diagnose adapter outcomes from the existing
workflow claim status, collector runtime status derivation, fact evidence, and
the optional host-owned status recorder.

Collector Deployment Evidence: this package adds no Helm template, Compose
service, ServiceMonitor, default component activation, OCI puller, or hosted
claim scheduler wiring. It is a core library path for explicit host runners;
remote Compose proof and hosted rollout remain gated by the follow-up
out-of-tree collector proof issues.

No-Observability-Change: the package records bounded SDK status records through
an injected `StatusRecorder` and otherwise relies on existing
`collector.ClaimedService` claim failure classes, workflow claim rows,
collector fact evidence, and `/admin/status` collector runtime derivation. It
adds no new metric labels, graph writes, queue domains, API routes, or MCP
tools.
