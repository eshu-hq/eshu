# internal/collector/gcpcloud/gcpruntime

Runtime wiring for the GCP Cloud Asset Inventory collector. This package turns the
fixture-driven `gcpcloud` parser, normalizer, redactor, and generation
accumulator into a `collector.Source` the shared hosted collector service can
poll and commit.

## Responsibilities

- `Source` implements `collector.Source`. Each `Next` yields one
  `collector.CollectedGeneration` for the next configured scope.
- Drains Cloud Asset Inventory pages through the `PageProvider` seam, accumulates
  `gcp_cloud_resource`, `gcp_tag_observation`, and
  `gcp_collection_warning` facts in a
  `gcpcloud.Generation`, and fences the generation with
  `gcpcloud.GenerationTracker` so a stale scan cannot replace current facts.
- Emits bounded-label telemetry through `gcpcloud.Metrics`: claim lifecycle,
  pages, page-token resumes, facts emitted by fact kind, warnings, and freshness
  lag from provider page read time.

## The PageProvider seam

`PageProvider.FetchPage` is the only transport boundary. Two implementations
ship in this slice:

- `FixturePageProvider` serves parsed pages from memory (`NewFixturePageProvider`)
  or from files (`NewFixturePageProviderFromFiles`). It performs no network call
  and backs every test plus the binary's offline smoke path. It enforces
  continuation-token matching so pagination resume is exercised honestly.
- `LiveClient` is the documented live gRPC/REST seam. It is intentionally
  **unimplemented** (`FetchPage` returns `ErrLiveClientNotImplemented`) and is
  **not wired as a default**, so the live path cannot make a Google Cloud call by
  accident. The real adapter, read-only credential resolution, retry, throttle,
  and backoff land in a later slice.

No code in this package and no test performs a live Google Cloud call.

## Configuration

`Config` is declarative: a collector instance id, a poll interval, and a list of
bounded `ScopeConfig` shards. `ScopeConfig` references its read-only credential
by **name** (`CredentialRef`) only; no secret material is stored. Scope identity
defaults to the contract form
`gcp:<parent_kind>:<parent_id>:<asset_family>:<content_family>:<location_bucket>`.

## Scope and stale handling

- One `Next` call yields one scope; when the scope batch is exhausted `Next`
  returns `ok=false` so the service waits for the next poll, then restarts.
- A generation rejected by a newer fencing token emits a single
  `gcp_collection_warning` (`warning_kind=stale_generation`,
  `outcome=stale`) and never emits resource facts.
- A continuation token the provider cannot resume becomes a
  `page_token_expired` partial warning instead of silent truncation.

## Deferred (not in this slice)

Direct/effective GCP tag APIs, IAM, relationship, DNS, and image-reference scan
emission, reducer admission, API/MCP readback, Helm values, environment-variable
contracts, and live Cloud Asset Inventory transport are deferred per
`docs/public/reference/gcp-cloud-collector-contract.md`. This package is runtime
scaffolding that is fixture-tested only.

## Performance and observability evidence

The collector runtime no-regression and observability evidence for this package
is recorded in `docs/public/reference/gcp-cloud-collector-contract.md` under
"Runtime Scaffolding Evidence".
