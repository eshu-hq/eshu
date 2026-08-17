# Evidence Bundle CLI

## Purpose

`evbundle` owns the logic behind `eshu evidence bundle export` and
`eshu evidence bundle validate`. It builds two kinds of `evidence_bundle.v1`
artifact -- the deterministic fixture demo bundle, and a live bundle composed
from a running stack's three status routes -- renders both as stable JSON, and
prints the pass/fail verdict for a bundle handed back to `validate`.

The artifact exists to be handed to someone else, so the package's job is
narrow on purpose: fetch, map, hand to `internal/evidencebundle`, and refuse
to return bytes that the composer's validation rejected.

## Ownership boundary

This package owns bundle *logic*: which status routes a live bundle reads,
how their decoded responses map onto an `evidencebundle.LiveSnapshot`, the
order of build -> validate -> stamp -> render, and the validation verdict
line. It does not own process wiring -- cobra flags, the API client, the
process clock, the exit-code mapping -- which stays in
`go/cmd/eshu/evidence_bundle_cmd.go` because `go/cmd/eshu` is `package main`
and nothing can import it. The wrapper also owns the `--scope` / `--live`
refusal, since that is a flag-combination check.

Composing the bundle itself belongs to `internal/evidencebundle`. This
package adapts three HTTP responses into that composer's input shapes and
never builds a `Bundle` literal of its own.

The `PipelineStatus` decode types here are this package's own, not shared with
`internal/cli/scan`'s `PipelineStatus`. That type serves the scan, first-run, and
hosted-setup families, which read a different subset of the same route; a
shared struct would tie what a share-safe artifact reports to changes those
families need.

## Exported surface

- `StatusFetcher` -- the one network capability needed, `Get(path, result)`;
  declared here at the point of use, satisfied by `cmd/eshu`'s `*APIClient`
- `IndexEndpoint`, `PipelineEndpoint`, `CollectorsEndpoint` -- the three
  stack-global status routes a live bundle reads
- `FetchLiveSnapshot` -- performs the three GETs and maps them; any route
  failing aborts the whole fetch
- `LiveSnapshotFromStatus` -- the pure mapping from three decoded responses to
  an `evidencebundle.LiveSnapshot`
- `ExportDemo`, `ExportLive` -- build, validate, stamp, render; both return
  no bytes at all when validation rejects
- `WriteBundle` -- writes to a path (mode `0600`) or to the supplied writer
- `ReadBundleInput` -- reads a bundle from a path or the supplied reader
- `ValidateBundle` -- decodes, validates, writes the verdict line, returns the
  reason
- The status decode types: `IndexStatus`, `QueueBlockage`,
  `SemanticExtractionState`, `SemanticProviderProfile`, `CollectorsResponse`,
  `Collector`, `PipelineStatus`, `PipelineHealth`, `PipelineQueue`,
  `PipelineGenerationHistory`, `PipelineStageSummary`,
  `PipelineDomainBacklog`, `PipelineScopeActivity`

See `doc.go` for the godoc contract.

## Dependencies

- `internal/evidencebundle` -- `BuildDemoBundle`, `BuildLiveBundle`,
  `Validate`, `StampValidation`, `RenderJSON`, and the `Live*Snapshot` input
  shapes. That package is a pure composer and dials nothing; this package is
  where the live path touches the network.
- Consumed by `go/cmd/eshu`: `evidence_bundle_cmd.go`.

No storage, graph, or queue package is imported, and no LLM provider is
reached. The live path's only outbound calls are the three status GETs.

## What the artifact screens, and what it does not

`ExportDemo` and `ExportLive` both run `evidencebundle.Validate` before
returning, and return `nil` bytes when it rejects. That check is a
**whole-document scan over the marshalled bundle**, not a field-name-keyed
redactor, so a sensitive value composed into an ordinary field is screened the
same as one under a suggestively named key. The patterns live in
`go/internal/evidencebundle/bundle.go`; grep the names below.

Rejected (the whole export fails, nothing is written):

- any canary in the shared `redact.HostedGovernanceRegistry()` under
  `SurfaceOnboardingArtifacts`
- a scheme-bearing private endpoint -- `privateEndpointPattern`
- a bare `host:port` for a loopback, RFC1918, link-local, `*internal*`, or
  `*.cluster.local` host -- `privateHostPortPattern`
- a portless private address, including `fc00::`/`fd00::` and
  `*.cluster.local` -- `privateAddressPattern`
- a credential-bearing URL, by `user:pass@` shape rather than by known value
  -- `credentialURLPattern`
- credential keywords with a non-numeric assigned value, `gh[pousr]_` tokens,
  and PEM private-key headers -- `credentialPattern`
- raw prompt or provider-response markers -- `rawPromptPattern`
- filesystem roots such as `/Users/`, `/etc/`, `~/`, `C:\` --
  `localPathPattern`

Not screened -- state these to anyone about to share a bundle:

- **A public hostname or public IP.** Only loopback, RFC1918, link-local,
  `*internal*`, and `*.cluster.local` shapes are listed. `https://acme.example.com`
  in a health reason exports as written.
- **A secret with no recognised shape.** `credentialPattern` keys on an
  assignment; free text such as `auth handle abc123` carries no `:` or `=` and
  passes. `bundle.go` also documents an accepted gap for a numeric-only
  `password=123456` outside JSON.
- **Anything an operator types into `--scope`.** The scope handle is content,
  not a label: it lands in `Identity.ScopeID` and in every reproduce call's
  `repo_id`. It goes through the same whole-document scan, so it is screened
  to exactly the level above and no further.
- **Free-text status values in general.** `health_reasons`,
  `semantic_extraction.reason`, and each provider profile's `reason` are
  copied through verbatim from the API; screening is by shape, and shape is
  not intent.

Two things the bundle deliberately never carries, which is why they need no
screening: the operator's `--service-url` / `ESHU_API_KEY` never enter a
bundle field (`Source.Repository` is the constant `live:stack`, and every
reproduce target is a fixed string), and this package calls no
`redactEndpoint`-style helper, so it does not inherit that helper's habit of
stripping userinfo while keeping a query string.

One surface that is **not** the artifact: a failed status fetch returns
`fetch <route>: <error>`, and Go's `*url.Error` prints the full request URL,
so the operator's own service URL -- userinfo included, if they put it there
-- appears in that message. `cmd/eshu`'s `main` writes it to stderr. It is
never written to the bundle, to `--out`, or to stdout.

## Telemetry

None. Both subcommands run inline with the CLI invocation; there is no
background stage to instrument. The live path's three GETs are observable on
the serving API's own request telemetry.

## Gotchas / invariants

- **Validate before stamp, always.** `StampValidation` writes
  `validation.status: passed`. Stamping a bundle whose `Validate` was skipped
  would certify a check that never ran, so both export paths stamp only after
  a nil return.
- **A failed export returns no bytes.** Callers must not write a partial
  rendering; `ExportDemo` and `ExportLive` return `nil` with their error.
- **Any failing status route fails the whole live export.** A partial fetch
  would publish zero counts as observed truth.
- **`QueueBlockage.Blocked` is a row count, not a flag.** The snapshot sums it
  across entries; counting entries would report a single heavily-gated domain
  as `1`. It deliberately does not reconcile with
  `domain_backlogs[].blocked`, which the status layer reports as the maximum
  among one domain's blockage rows.
- **`domain_backlogs_truncated` has to travel.** The status layer caps
  `domain_backlogs` at `status.DefaultOptions().DomainLimit` (5) and sets that
  flag; the snapshot carries it so `evidencebundle` can mark
  `bounds.truncated`. Dropping it makes a capped list read as a complete
  enumeration, which is a wrong answer about the stack.
- **Live bundles carry no repository scope.** All three routes are
  stack-global, so `ExportLive` is called with an empty scope and
  `evidencebundle` labels the artifact `live:local`. The wrapper refuses
  `--scope` with `--live` rather than mislabelling.
- **The clock is a parameter, and it is a function.** `ExportLive` takes
  `now func() time.Time`; nothing here calls `time.Now`, which is what makes a
  bundle reproducible in a test. It is a thunk rather than an already-read
  `time.Time` so that `ExportLive` -- not its call site -- decides when the
  clock is read. It reads it once, after `FetchLiveSnapshot` returns, so
  `identity.created_at` is when the evidence finished being read. Passing
  `now()` at the call site instead compiles and looks identical, but stamps the
  fetch-START time and drifts earlier by the whole fetch duration on a slow
  stack. `TestExportLiveStampsCreatedAtAfterTheFetch` is the guard.

## Related docs

- `go/internal/evidencebundle/README.md` -- the composer and the validation
  contract this package depends on
- `go/internal/query/evidence_bundle_live.go` -- the API route that composes
  the same bundle from a typed `status.Report`; `cmd/eshu`'s
  `evidence_bundle_api_parity_test.go` holds the two readings together
