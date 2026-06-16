# internal/collector/gcpcloud/gcpruntime

Runtime wiring for the GCP Cloud Asset Inventory collector. This package turns the
fixture-driven `gcpcloud` parser, normalizer, redactor, and generation
accumulator into a `collector.Source` the shared hosted collector service can
poll and commit.

## Responsibilities

- `Source` implements `collector.Source`. Each `Next` yields one
  `collector.CollectedGeneration` for the next configured scope.
- Drains Cloud Asset Inventory pages through the `PageProvider` seam, accumulates
  `gcp_cloud_resource`, `gcp_cloud_relationship`,
  `gcp_tag_observation`, `gcp_iam_policy_observation`, `gcp_dns_record`,
  `gcp_image_reference`, and `gcp_collection_warning` facts in a
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
- `LiveClient` is the explicitly injected live REST seam for
  `assets.list`. It requires a caller-supplied read-only token source, bounds
  page size, response bytes, timeout, retry attempts, backoff, and asset-family
  filters, and converts expected provider coverage gaps into
  `gcp_collection_warning` facts. It is **not wired as a default**, so the
  command path still cannot make a Google Cloud call by accident.

No test performs a live Google Cloud call; live-client tests use local HTTP
servers.

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

## Deferred (not in this package)

Direct/effective GCP tag APIs, command wiring for live transport,
claim-enabled scheduler activation, Helm values, environment-variable contracts,
ServiceMonitor wiring, and sanitized target smoke proof are deferred per
`docs/public/reference/gcp-cloud-collector-contract.md`. Shared cloud inventory
admission and API/MCP readback for `gcp_cloud_resource`, tag evidence admission,
image identity admission, relationship resolution, and IAM trust facts are
implemented outside this package and remain separate from live provider
activation.

## Performance and observability evidence

The collector runtime no-regression and observability evidence for this package
is recorded in `docs/public/reference/gcp-cloud-collector-contract.md` under
"Runtime Scaffolding Evidence".
