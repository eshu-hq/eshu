# Collector Extension Host

`extensionhost` adapts public `collector-sdk/v1alpha1` extension results into
the core `collector.ClaimedService` path. A manifest that explicitly configures
`collector-sdk/v1alpha2` handshakes on the alpha2 protocol instead; the
v1alpha1 default is unchanged.

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

Producer-grant decision telemetry (#6726): the host is the owning decision
site for the activation and emission stages of producer-grant admission.
`Config.GrantObserver` (a `component.GrantObserver`) receives one decision per
core-owned fact kind at `NewSource` and one per distinct core kind per result
from the per-emission recheck (`Source.validateGrantCoverage`), whether the
grant allows or denies; a denial still fails the result closed (terminal
`InvalidResult`, zero facts). `GrantTelemetry` is the production observer: it
records `eshu_dp_component_producer_grant_decisions_total` (labels `decision`,
`stage`, `reason`, `fact_kind`; producer id is never a label), adds the
`component.producer_grant.decision` span event on the active span, and logs
`producer_grant.*` keys (WARN on deny, INFO on install/activation allow, no
per-emission allow log). `Config.LiveGrants` returns an error so an unreadable
registry is a distinct `grants_unreadable` deny rather than a silent nil.

Grant Decision Benchmark Evidence: `go test ./internal/collector/extensionhost
-run '^$' -bench 'GrantCoverageRecheck' -benchmem -count=5` on the granted
core-kind result with a 50-grant set: the recheck is 6 allocs/op and
3504 B/op before the change and with no observer configured, and 7 allocs/op
and 3520 B/op with `GrantTelemetry` attached over an unsampled context (the
one extra allocation is the OTEL SDK counter add; the recorder itself adds
none). `ns/op` on the shared development host was dominated by load and is not
quoted; see the change's report for the measurement conditions.

No-Observability-Change: beyond the producer-grant decision signals above, the
package records bounded SDK status records through an injected
`StatusRecorder` and otherwise relies on existing `collector.ClaimedService`
claim failure classes, workflow claim rows, collector fact evidence, and
`/admin/status` collector runtime derivation. It adds no graph writes, queue
domains, API routes, or MCP tools.

Number Fidelity No-Regression Evidence: `go test
./internal/collector/extensionhost -run
'^TestProcessResultNumbersSurviveClaimedSource$' -count=1` failed on baseline
`ffca930a8b25` because JSON integer `9007199254740993` became
`9007199254740992`. It passes after `ProcessRunner` decodes payload numbers
without a float64 conversion. The subprocess, SDK validator, claimed source,
fact envelope clone, and JSON consumer retain the exact integer, negative
integer, int64 boundary, nested array, and decimal values. The full package
test, race test, and vet also pass. The separate public typed-consumer tests
preserve an exact issue count and nested Kubernetes ports through the
subprocess, grant/schema validation, claimed source, and envelope clone.

`ProcessRunner` and `OCIRunner` now use the same bounded result decoder. The
OCI adapter test supplies an injected runtime executor and checks exact
numbers through grant/schema validation, the claimed source, envelope clone,
and public typed scalar and nested-list consumers. Shared decoder tests cover
unknown fields, empty or malformed input, and trailing values or junk. Existing
runner tests cover output limits, cancellation, and executor failures. The OCI
test does not run a container. Database reload, graph, and deployed consumer
compatibility remain unverified.

Number Fidelity Benchmark Evidence: on linux/amd64, Intel i7-8700K, Go
1.26.10, the same `ProcessRunner.RunCollector` subprocess benchmark ran five
100-operation samples per shape before (`ffca930a8b25`) and after the decoder
change (`d1ab1d8187`). Numeric-array JSON results of about 1 KiB measured
5.718 to 5.663 ms/op at the sample medians, with sample maxima 5.771 to
5.719 ms/op; bytes/op rose 92,662 to 93,174 and allocs/op stayed 258. About
64 KiB measured 10.607 to 7.126 ms/op at the sample medians, with sample
maxima 10.913 to 7.325 ms/op; bytes/op rose 908,387 to 941,155 and allocs/op
stayed 8,338. These are aggregate benchmark samples, not individual request
p95 values. The additional bytes scale with decoded numeric payload and remain
inside the existing 4 MiB stdout cap. No queue or row counts apply to this
standalone subprocess decode.

Number Fidelity No-Observability-Change: decoding changes only the in-memory
numeric representation. The existing process and OCI adapter errors, bounded
SDK statuses, claim failure classes, and collector fact evidence remain the
operator signals; the focused tests prove accepted results reach the claimed
source without a new status or failure path.
