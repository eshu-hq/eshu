# Collector Extension Host

`extensionhost` adapts public `collector-sdk/v1alpha1` extension results into
the core `collector.ClaimedService` path.

The package owns only the intake boundary:

- build a bounded JSON request from a durable `workflow.WorkItem`
- launch a host-provided runner: `ProcessRunner` (local process) or `OCIRunner`
  (digest-pinned OCI artifact under container isolation)
- validate the result with `sdk/go/collector`
- validate fact payloads against factschema fixture-pack schemas when the
  manifest declares `payloadSchemaRef`
- compare the returned claim identity with the host claim
- map accepted SDK facts into internal `facts.Envelope` values
- classify retryable, terminal, invalid-result, and identity-mismatch outcomes

It does not receive or pass Postgres, graph, reducer, API, or MCP handles to an
extension. Claim mutation, stale-fence handling, retry budgets, and final commit
remain owned by `collector.ClaimedService` and its claim-aware committer.

No-Regression Evidence: `go test ./internal/collector/extensionhost -count=1`
proves the host passes only a bounded JSON claim/config/contract document,
validates SDK output and declared payload schemas before fact mapping,
deduplicates exact SDK facts, maps partial and unchanged states without direct
graph writes, and returns retryable/terminal classified errors for existing
workflow claim mutations.

Collector Performance Evidence: `go test ./internal/collector/extensionhost
-count=1` exercises the local host path without live network, database, graph,
or reducer handles. The process and OCI runners both enforce bounded stdout and
stderr and decode exactly one SDK result, the
source performs one SDK validation pass per claimed result plus one payload
schema validation pass when `payloadSchemaRef` is declared, and exact duplicate
facts are collapsed before commit handoff so retry delivery does not amplify
same-generation fact streams.

Benchmark Evidence: `GOCACHE=/tmp/eshu-4801-bench go test
./internal/collector/extensionhost -run '^$' -bench
'^BenchmarkSourcePayloadSchemaValidation$' -benchmem -count=5` measured the
schema-ref validation pass for one accepted extension fact at 30,597-36,479
ns/op, 25,550-25,553 B/op, and 407 allocs/op on darwin/arm64 Apple M5 Max.
This path runs only for extension results whose manifest declares
`payloadSchemaRef`; provenance-only namespaced facts keep the prior validation
path.

Collector Observability Evidence: the same focused test proves SDK status
records are copied into bounded `StatusRecord` values and every final state
continues through `collector.ClaimedService` failure classes, claim rows, and
fact commit counters. Operators diagnose adapter outcomes from the existing
workflow claim status, collector runtime status derivation, fact evidence, and
the optional host-owned status recorder.

Collector Deployment Evidence: this package adds no Helm template, Compose
service, ServiceMonitor, default component activation, or hosted claim
scheduler wiring. `OCIRunner` delegates the digest-pinned image pull and run to
the host container runtime (`docker`/`podman`) rather than embedding a registry
client, and `cmd/collector-component-extension` sources the image only from the
component's verified manifest artifact. It is a core library path for explicit
host runners; publishing the reference image and remote Compose proof remain
gated by the follow-up out-of-tree collector proof issues.

No-Observability-Change: the package records bounded SDK status records through
an injected `StatusRecorder` and otherwise relies on existing
`collector.ClaimedService` claim failure classes, workflow claim rows,
collector fact evidence, and `/admin/status` collector runtime derivation. It
adds no new metric labels, graph writes, queue domains, API routes, or MCP
tools.
