# Collector Extension Host

`extensionhost` adapts public `collector-sdk/v1alpha1` extension results into
the core `collector.ClaimedService` path. A manifest that explicitly configures
`collector-sdk/v1alpha2` handshakes on the alpha2 protocol instead; the
v1alpha1 default is unchanged. A manifest that explicitly configures
`collector-sdk/v1alpha3` additionally admits versioned multi-call pagination:
the satellite splits a result that would not fit one bounded stdout write
into pages carrying the resume cursor (`Result.Page`), and the host
re-invokes the runner with the cursor until a final page closes the
generation, reassembling one identical fact set. Legacy single-shot
satellites are unaffected: runners without paged support keep the exact
single-call path, and the first paged request stays byte-identical to the
legacy request. That single-call path cannot fetch later pages, so a result
that carries a page envelope there fails terminal as `invalid_result`
(`TestNextClaimedFailsClosedOnPageFromSingleCallRunner`) instead of committing
one page as the whole generation. A paged runner returns the page on
`CallResult.Page`; a page on the embedded SDK result fails the same way
(`TestCollectPagesRejectsPageOnEmbeddedResult`).

The package owns only the intake boundary:

- build a bounded JSON request from a durable `workflow.WorkItem`
- launch a host-provided runner: `ProcessRunner` (local process) or `OCIRunner`
  (digest-pinned OCI artifact under container isolation)
- validate the result with `sdk/go/collector`
- validate fact payloads against factschema fixture-pack schemas when the
  manifest declares `payloadSchemaRef`
- compare the returned claim identity with the host claim
- reassemble v1alpha3 pages with gapless sequence, pinned generation, and
  declared-total accounting (bounded by `Config.MaxResultPages`, default 64)
- map accepted SDK facts into internal `facts.Envelope` values
- classify retryable, terminal, invalid-result, and identity-mismatch outcomes
- carry the extension's provider retry delay on retryable results
- report every SDK status row and every claim result through `StatusTelemetry`

It does not receive or pass Postgres, graph, reducer, API, or MCP handles to an
extension. Claim mutation, stale-fence handling, retry budgets, and final commit
remain owned by `collector.ClaimedService` and its claim-aware committer.

Host-minted GitHub App tokens (#9182 lane A, wire shape per arbiter):
`Config.TokenProvider`, when set, mints one installation token per opted-in claim
host-side into the per-claim request config under `host_github_app_token` as
`{token, scope_id}`; a static `targets` entry with `auth_mode: host_minted` for the claimed
scope mints (a GHSA window claim inherits its anchor's opt-in, #10533), as does any claim carrying a host-resolved derived target; the App private key
never crosses the stdio boundary. Other claims keep the static path with the provider never
consulted. A mint denial
fails the claim terminal (`auth_denied`) before the satellite launches; a transient
mint fault stays retryable, and with no provider
the config passes through byte-identically (the `token_env` escape hatch).

Derived targets (#10027): `Config.DerivedTargetResolver`, when set, adds a
derived scope's target to the per-claim config under `host_derived_target`. A Go
reachability scope id holds only the repository id, so this object carries what
to fetch: `scope_id`, `repository_id`, `source_commit_sha`, and `clone_url`, from
the run's `requested_scope_set`. A missing run or an unusable entry fails the
claim terminal before launch; a store fault stays retryable. Other scopes read
nothing and stay byte-identical. Static config defining the key fails `NewSource`,
so config cannot forge a commit or a remote. The generation the claim commits carries
the target's commit in `SourceCommitSHA` (#10055), so the coordinator's commit-skew
fence can compare it without reading fact payloads. Other scopes stay unstamped.

Installation selection (#10027, #10533): an exact scope match in
`AppTokenProviderConfig.Installations` wins and reads nothing; a GHSA window then tries its anchor. Otherwise
`InstallationKey` resolves the claim to `github-owner:<host>/<owner>` from the same
validated entry. Only Go reachability scopes resolve a key, so an owner key cannot
serve another claim. The host is part of the key because the token goes to the
remote the owner came from: an owner-only key would mint a mapped owner's token
for a look-alike remote on another host. Owner keys match case-insensitively. A
store fault while reading the key is retryable with its text dropped.

Claim scope query transport (#9432): `Config.ScopeQueryResolver`, when set,
delivers the run's `requested_scope_set` entry for the claimed scope onto the
satellite claim as `Claim.ScopeQueries` (at most `MaxScopeQueries` entries).
The resolver returns nil for every legacy scope id, and `omitempty` keeps
those claims byte-identical to the legacy shape. A fail-closed resolver
error (missing entry, unparsable entry, ambiguous scope, or no queries)
fails the claim terminal with class `invalid_claim` before the satellite
launches; transient store faults stay retryable and never reach this path. The
returned-claim echo compares `ScopeQueries` strictly: a stripped, injected,
or reordered list fails with `identity_mismatch`.
`cmd/collector-component-extension` sets the resolver from
`extensionhost/vulnerability.SlotScopeQueryResolver`; the generic host stays
kind-agnostic.

The in-tree SDK's [validation contract](../../../../sdk/go/collector/README.md#validation)
admits only the documented Kubernetes posture values in their exact fact
contexts. `ProcessRunner` preserves those values through JSON transport. That
key check does not grant producer authority: optional payload-schema validation
and the live producer-grant check still run before fact mapping.

Collector Performance Evidence: `go test ./internal/collector/extensionhost
-count=1` exercises the local host path without live network, database, graph,
or reducer handles. The process and OCI runners both enforce bounded stdout and
stderr and decode exactly one SDK result, the
source performs one SDK validation pass per claimed result plus one payload
schema validation pass when `payloadSchemaRef` is declared, and exact duplicate
facts are collapsed before commit handoff so retry delivery does not amplify
same-generation fact streams.

Collector Observability Evidence: the same focused test proves SDK status
records are copied into bounded `StatusRecord` values and every final state
continues through `collector.ClaimedService` failure classes, claim rows, and
fact commit counters. Operators diagnose adapter outcomes from the existing
workflow claim status, collector runtime status derivation, fact evidence, and
the `StatusTelemetry` status recorder described below.

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

## Status rows and provider retry delay (#8908)

Extracted collectors emit no telemetry of their own; their SDK status rows are
the operator contract. `StatusTelemetry` is the production `StatusRecorder`,
and `cmd/collector-component-extension` sets it. It also implements the
optional `ResultRecorder` interface, so `Source.NextClaimed` reports exactly
one `ResultRecord` per call on every exit path: SDK results (`complete`,
`unchanged`, `partial`, `retryable`, `terminal`) with summed status counts, and
host verdicts (`invalid_claim`, `launch_failure`, `invalid_result`,
`identity_mismatch`, `status_record`, `source_state_record`) with the host
failure class and the state `ClaimedService` applies. Host verdicts carry no
status counts because an unvalidated result's statuses are not trusted.
`ResultRecord` holds no scope, instance, work item, generation, or URL value by
construction.

Signals (labels `collector_kind`, `state`, `status_class`, `failure_class`
only; see [Telemetry Metrics](../../../../docs/public/reference/telemetry/metrics.md)):
`eshu_dp_component_extension_results_total`,
`eshu_dp_component_extension_status_records_total`,
`eshu_dp_component_extension_reported_facts_total`,
`eshu_dp_component_extension_reported_warnings_total`,
`eshu_dp_component_extension_source_latency_seconds`, and the
`eshu_dp_component_extension_retry_after_seconds` gauge (the delay the extension
requested on the latest result for the kind, after the cap; `0` when it was not
retryable; the request, not the applied delay). One log line
per result (`component extension result`, keys `collector_kind`,
`failure_class`, `extension_status.*`) logs at WARN for retryable, terminal,
and host verdicts and at INFO otherwise. Failure classes past 64 distinct
values per process fold to `other`. Failure classes are collector-defined, so
the cap is per process and which classes fold depends on arrival order: the
first 64 seen keep their own label for the life of the process.

A collector may refine a class with one dot-separated sub-reason,
`<class>.<reason>` (for example `invalid_config.target_scope_mismatch`). The
host treats the whole token as the class and changes nothing for it:
`failureClassFromStatus` returns it verbatim, `extensionFailure.Error()` puts it
in the failure message, `ClaimedService` persists it unsanitized as
`last_failure_class`, and the metric label admits it like any other class.
`TestValidateBoundedStatusesRefinedClass`, the refined case in
`TestSourceRoutesRetryableAndTerminalResultsThroughClaimFailures`, and the
telemetry and `ClaimedService` pins keep that true. A value rejected by the
host's own bounded checks never reaches the error verbatim: the host reports
the rule and a bounded description (byte length plus a short prefix from the
allowed alphabet, e.g.
`status.failure_class is not bounded (105 bytes, prefix "invalid_config.a")`),
and the same holds for the continuation-state rejection in `checkPageEnvelope`.
`TestValidateBoundedStatusesErrorOmitsRawValue` and
`TestCheckPageEnvelopeContinuationErrorOmitsRawState` pin that. Rejections
that surface the SDK validator's wording keep the SDK error text; bounding
those echoes is separate follow-up work, not part of this change.

Bounded Rejection No-Regression Evidence (#9310): the bounded description
replaces the raw echo only on the rejection path; the accept path is the same
comparisons as before. `go test ./internal/collector/extensionhost -run '^$'
-bench '9310' -benchmem -count=5` (go1.26.10, linux/arm64, shared host) built
the old `%q` error against the new `describeRejectedValue` error for a
141-byte hostile value. Old: 1,079-1,149 ns/op, 656 B/op, 5 allocs/op. New:
511-733 ns/op, 208 B/op, 7 allocs/op (the first run measured 1,157 ns/op
cold; the two extra allocs are the prefix buffer and format args). The
accepted-class check measures 486-495 ns/op with zero allocs. Safe because the
rejection runs at most once per invalid result on the terminal-failure path,
never in the per-page streaming loop.

Bounded Rejection Observability Evidence (#9310): for the host's
bounded-class and continuation-state rejections, the `invalid_result` cause
changes from the raw value to the rule plus the bounded description, e.g.
`status.failure_class is not bounded (105 bytes, prefix "invalid_config.a")`
and `extension result must not request continuation (state 200 bytes, prefix
"zzzzzzzzzzzzzzzz")`. Operators match on the rule text and the class, never on
extension-supplied substrings; log lines and stored failure messages keep the
same keys. An alert that matches the
base class exactly must match the prefix instead (see the
[hosted extension policy](../../../../docs/public/operate/hosted-extension-policy.md)).

A `retryable` result's classified failure carries the largest
`status.retry_after_seconds` as `RetryAfterDelay()`, which
`collector.ClaimedService` uses for `VisibleAt` and for
`eshu_dp_workflow_claim_provider_throttle_total{outcome="retry_after_honored"}`.
For extension collectors that counter counts every retryable result that asked
the host to back off (`retry_after_seconds > 0`, which the SDK requires on
every retryable result), not only HTTP rate limiting; the metric name is kept.
The delay is clamped to `MaxRetryAfterDelay` (one hour, the GitHub App
token-mint precedent): the SDK only requires `retry_after_seconds >= 0`, so an
unclamped value could hide a work item for years or overflow `time.Duration`.
Terminal results never carry a delay. The SDK requires `retry_after_seconds`
>= 1 on the failure status every retryable result carries, so a validated
retryable result always requests at least one second; zero only appears on
other status rows and means no request. A negative value fails the result as a
terminal `invalid_result`. The result log line shows the clamp:
`retry_after_seconds` is the capped request and `retry_after_requested_seconds`
is the raw request.

Both the gauge and the log key record the request, not the applied delay. They
are written inside `NextClaimed`, before `ClaimedService` decides. It then
applies the larger of its poll interval (`ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL`)
and the request, so a request below the poll interval shows a delay that was
not applied. On the last attempt (`AttemptCount >= MaxAttempts`) it applies
none: the claim fails terminally, counted by
`eshu_dp_workflow_claim_attempt_budget_exhausted_total`, while the gauge, the
log line, and `results_total{state="retryable"}` still show the extension's
request. A fenced write that loses its lease applies nothing either.
`results_total{state}` is the extension's result state, not the durable claim
outcome. `TestResultRecordRetryAfterIsTheRequestNotTheAppliedDelay` pins this.

Host Token Provider No-Regression Evidence (#9182 lane A, gate per #10483):
`go test ./internal/collector/extensionhost -count=1` and the same package
with `-race` both pass at this HEAD, covering opted-in mint injection, the
terminal `auth_denied` denial mapping (including an unbounded-error canary),
retryable backoff propagation, construction-time reserved-key rejection,
byte-identical `token_env` passthrough with no provider, and a never-consulted
provider for non-opted-in claims. Baseline is the `origin/main` claim path:
without a provider the gate adds one nil check per claim; with a provider it adds
one short targets scan (first scope match decides) and mints only for opted-in
claims. A real claim launches a process or container (milliseconds), so the scan
plus one bounded mint call are not a measurable share.

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
p95 values. The additional bytes scale with decoded numeric payload. Stdout input
remains capped at 4 MiB; total decoder allocations were measured only at the
stated shapes. No queue or row counts apply to this
standalone subprocess decode.

Number Fidelity No-Observability-Change: decoding changes only the in-memory
numeric representation. The existing process and OCI adapter errors, bounded
SDK statuses, claim failure classes, and collector fact evidence remain the
operator signals; the focused tests prove accepted results reach the claimed
source without a new status or failure path.

Satellite Env Scrub No-Regression Evidence: the per-claim child-env filter
walks the inherited host environment once (tens of entries) plus the
configured ScrubEnv names (three App credential variables), keeping every
entry whose name is not an exact scrubbed match; explicit Env entries append
afterwards unchanged. No storage, queue, lease, graph, or network path is
touched, and the OCI adapter (explicit env only) is not modified.
`go test -count=1` and `-race` pass for `internal/collector/extensionhost`
and `cmd/collector-component-extension`, including end-to-end tests that
exec a real child and assert the scrubbed name is absent from its
environment, passthrough entries survive, and explicit Env overrides.
Behavior is byte-identical except for the three withheld credential names,
which no satellite legitimately reads: the host injects the minted token
over stdio.

Satellite Env Scrub No-Observability-Change: the filter emits no metric,
span, log, or status of its own. Operator signals are unchanged: claim
failure classes, bounded SDK statuses, and existing collector fact evidence.
A satellite missing an expected variable fails through its own existing
result and error path, not a new one.

## Satellite stderr relay (#9823)

A satellite that fails a claim as `input_limit_exceeded` prints three byte
figures on its stderr record. The host relays exactly `inflated_bytes`,
`budget_bytes`, and `compressed_bytes`, as integers, onto the one
`component extension result` log line, and only when the claim's final
assembled result is neither `complete` nor `unchanged`. It never relays a
satellite string, a raw line, or any other key. The allowlist, bounds,
benchmark evidence, and operator read path live in
[STDERR_RELAY.md](STDERR_RELAY.md).

## Claim outcomes and host-owned source state (#9420)

`Config.ClaimOutcomeRecorder` is an optional seam for durable state the host
owns on behalf of an extension; the extension never sees it, so no storage
handle crosses the stdio boundary. `cmd/collector-component-extension` sets it
only for the `vulnerability_intelligence` kind
(`collector/extensionhost/vulnerability`, which writes
`vulnerability_source_states`); every other kind leaves it nil.

- `NextClaimed` calls `RecordClaimOutcome` exactly once on every exit path,
  after the outcome is decided and before the `ResultRecord`, with a
  `ClaimOutcome` that carries the work item, origin, state, failure class,
  terminal flag, capped retry request, partial flag, fact and warning counts,
  the host attempt time, and the generation's observed time.
- A completed outcome (`complete`, `unchanged`, `partial`) is stashed by work
  item id. `Source.ObserveClaimedGenerationCommitted`
  (`collector.ClaimedGenerationCommitObserver`) hands it to
  `RecordClaimCommitted` after `ClaimedService` commits the generation, or
  resolves an unchanged one. The stash is cleared on every `NextClaimed` exit
  for the item and capped at 256 entries (oldest attempt evicted).
- A refused commit (#10037) takes the stash the same way, through
  `Source.ObserveClaimedGenerationRefused`
  (`collector.ClaimedGenerationRefusalObserver`), and reaches
  `RecordClaimRefused` instead of `RecordClaimCommitted`. Its error is non-fatal
  too.
- An attempt-row error on a completed outcome fails the claim retryable with
  `source_state_record`; an error on a failed outcome is joined to the claim
  error and never changes its class or terminality; a post-commit error is
  non-fatal.

## Core commit handler (#8877)

`CommitHandler` serves the core side of `POST /workflow.v1/CommitClaimFacts`: admission first, then `collector.CommitClaimFacts` with a per-kind scope builder mirroring `mapping.go`; `{"ok":true}` only after COMMIT.
