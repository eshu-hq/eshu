# internal/collector/gcpcloud/gcpruntime

Runtime wiring for the GCP Cloud Asset Inventory collector. This package turns the
fixture-driven `gcpcloud` parser, normalizer, redactor, and generation
accumulator into a `collector.Source` the shared hosted collector service can
poll and commit.

## Responsibilities

- `Source` implements `collector.Source` for fixture mode and
  `collector.ClaimedSource` for claim-driven mode. `Next` yields one configured
  scope per poll; `NextClaimed` resolves one workflow work item into one
  authorized scope and uses the work item's generation and fencing token.
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

`PageProvider.FetchPage` is the CAI transport boundary. Two implementations ship
in this slice:

- `FixturePageProvider` serves parsed pages from memory (`NewFixturePageProvider`)
  or from files (`NewFixturePageProviderFromFiles`). It performs no network call
  and backs every test plus the binary's offline smoke path. It enforces
  continuation-token matching so pagination resume is exercised honestly.
- `LiveClient` is the explicitly injected live REST seam for
  `assets.list`. It requires a caller-supplied credential whose IAM grants are
  read-only, bounds page size, response bytes, timeout, retry attempts, backoff,
  and asset-family filters, and converts expected provider coverage gaps into
  `gcp_collection_warning` facts. It is **not wired as a default**, so the
  command path can use it only in explicit claimed-live mode; it is still not a
  default provider.
- `LiveClient.QuotaProjectID` is an explicit, name-only opt-in. When set, it is
  sent as the `x-goog-user-project` header on `assets.list` requests. Leave it
  empty for service-account/Workload Identity Federation ADC (the identity is
  its own quota consumer). Set it when the resolved ADC is a user credential
  (e.g. local `gcloud auth application-default login`), which otherwise gets a
  403 quota-project error from Cloud Asset Inventory. The claimed command
  reads it from `ESHU_GCP_COLLECTOR_QUOTA_PROJECT_ID`.

  No-Regression Evidence: the header set is a single conditional
  `strings.TrimSpace` plus one `http.Header.Set` per `assets.list` attempt,
  gated on `QuotaProjectID != ""`; when unset (the default — SA/WIF ADC) this
  is a no-op branch taken once per page fetch, no allocation or measurable
  cost added to the existing retry/backoff loop. No-Observability-Change:
  extraction/paging telemetry is unaffected — the header only changes which
  billing project Cloud Asset Inventory attributes quota usage to; it does
  not change request count, page shape, or error/warning classification.

`TagProvider.FetchTagPage` is the opt-in Resource Manager tag API boundary.
`LiveClient` implements it for `tagBindings.list` and `effectiveTags.list`.
Scopes call it only when `DirectTagsEnabled` or `EffectiveTagsEnabled` is true;
tag values are fingerprinted before facts are emitted, and effective tags carry
bounded direct/inherited state.

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
- One `NextClaimed` call validates collector kind, source system, collector
  instance id, scope id, generation id, source run id, and positive fencing
  token before collecting. Unauthorized scope claims fail without logging
  configured parent ids or credential names.
- A generation rejected by a newer fencing token emits a single
  `gcp_collection_warning` (`warning_kind=stale_generation`,
  `outcome=stale`) and never emits resource facts.
- A continuation token the provider cannot resume becomes a
  `page_token_expired` partial warning instead of silent truncation.

## Deferred

Sanitized target smoke proof is deferred per
`docs/public/reference/gcp-cloud-collector-contract.md`. Shared cloud inventory
admission and API/MCP readback for `gcp_cloud_resource`, tag evidence admission,
image identity admission, relationship resolution, and IAM trust facts are
implemented outside this package and remain separate from live promotion.

## Performance and observability evidence

The collector runtime no-regression and observability evidence for this package
is recorded in `docs/public/reference/gcp-cloud-collector-contract.md` under
"Runtime Scaffolding Evidence".
