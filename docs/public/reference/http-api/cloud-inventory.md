# Cloud Inventory And Resource Paging

## Cloud Inventory Readback

`GET /api/v0/cloud/inventory` returns reducer-owned canonical
`reducer_cloud_resource_identity` rows (one per `cloud_resource_uid`). It is
filterable by `provider` (aws/gcp/azure), a scope selector, and
`management_origin` (declared/applied/observed). Results are paginated via
`limit` and `cursor` parameters. `local_lightweight` returns
`unsupported_capability`.

The scope selector is one of:

- `scope_id` -- the exact canonical ingestion scope id. A scope is one
  collector partition, not the whole provider account: for AWS specifically,
  the collector claims work per account+region+service, so one AWS account can
  span many scope ids (`go/internal/collector/awscloud/awsruntime/source.go`).
  `scope_id` takes precedence when given alongside an account selector.
- `account_id` (AWS), `project_id` (GCP), or `subscription_id` (Azure) -- the
  raw provider account/tenant identifier. **Requires the matching `provider`
  exactly** (`account_id` requires `provider=aws`, `project_id` requires
  `provider=gcp`, `subscription_id` requires `provider=azure`; any other
  provider value is rejected). Unlike
  `scope_id` this is **not** compared against the scope id itself (#5238:
  earlier revisions did, which silently matched zero rows for any real
  multi-scope account, on every provider); it resolves against the canonical
  payload's normalized `account_id` field, which the reducer writes from the
  admitting source fact's own identity field (AWS `account_id`, GCP
  `project_id`, Azure `subscription_id` -- see `go/internal/reducer/
  cloud_inventory_admission_writer.go`), and therefore returns every canonical
  row across every scope that shares that identifier. AWS `account_id` and
  Azure `subscription_id` are required identity fields on every admitting
  source fact, so this always resolves for those two providers. GCP
  `project_id` is genuinely **optional** (`sdk/go/factschema/gcp/v1/
  resource.go`): an organization- or folder-level Cloud Asset Inventory asset
  has no project. The reducer derives it from the asset's `full_resource_name`
  when a `projects/<id>` segment is present (the common, project-scoped case);
  a resource with no such segment has no `account_id` value and is correctly
  absent from every `project_id`-filtered read while remaining visible under
  an unscoped `provider=gcp` read.

**The matching provider is required alongside an account alias -- not just
any provider.** `account_id`, `project_id`, and `subscription_id` all resolve
against the SAME shared canonical `account_id` payload key, with no provider
baked into the predicate itself. AWS account ids and GCP project *numbers*
(as distinct from project IDs) are both plain decimal strings, and the GCP
derivation above can populate `account_id` from a numeric
`full_resource_name` segment -- so a caller who omits `provider`, or supplies
the *wrong* provider for the alias used, risks a genuine cross-provider
collision: one `account_id` value silently matching another provider's
unrelated resource for an all-scopes caller. Requiring a provider narrows the
blast radius but does not by itself prevent the mismatch --
`provider=gcp&account_id=...` would otherwise still be accepted and could
resolve against the GCP resource whose normalized `account_id` happens to
equal the supplied value, even though `account_id` is documented as the
AWS-specific alias. `GET /api/v0/cloud/inventory` and MCP
`list_cloud_resource_inventory` both reject: an account alias supplied
without `provider`; and an account alias supplied with a provider other than
its documented match (`account_id`→`aws`, `project_id`→`gcp`,
`subscription_id`→`azure`) -- both as `invalid_argument` (HTTP 400) -- rather
than searching across, or silently resolving against, another provider's
keyspace.

**Rollout window:** `account_id`/`project_id`/`subscription_id` only resolve
against rows admitted by the reducer AFTER this fix deploys. A scope's
currently-active generation -- the one that was live at deploy time -- was
admitted by the OLD reducer code and carries no `account_id` key on its
canonical payload at all, so it will not match any `account_id`/`project_id`/
`subscription_id` filter (though `scope_id` and unfiltered/`provider`-only
reads are unaffected) until that scope's next collector sync activates a new
generation. No forced re-sync is required as a deploy step: the normal
scheduled collector cadence for that scope closes the gap on its own next run.
An operator who needs `account_id` filtering to be authoritative immediately
after deploy should trigger a fresh sync for the scopes they care about rather
than wait for the next scheduled run.

**Distinguishing "no such account" from "not yet re-admitted."** Without more,
a zero-row response to an `account_id`/`project_id`/`subscription_id`-filtered
request is ambiguous between two very different states: the account genuinely
does not exist, or the account's data is still behind the rollout window
above. To resolve this, a zero-result account-alias-filtered call runs one
additional bounded Postgres check (never on the unfiltered/`scope_id` path,
and never when the alias filter already matched rows) for whether any
canonical row in the same provider/access scope predates the rollout. The
result is surfaced as a `warning_flags` array on the response:

- `account_alias_rollout_gap` — a pre-rollout row exists in scope; the zero
  rows do **not** prove the account does not exist, and the answer will become
  authoritative once that scope's next collector sync re-admits it.
- `account_alias_rollout_gap_check_failed` — the disambiguation check itself
  could not run; the primary (empty) result still stands, but the check
  could not confirm or rule out a rollout gap.
- Absent — the check ran and found no pre-rollout row in scope, so the zero
  rows are a genuine no-such-account/no-such-scope result.

`warning_flags` is never present for a `scope_id`-filtered or unfiltered
request, nor for any account-alias-filtered request that already returned at
least one resource, because the disambiguation query only fires for the one
case a caller cannot otherwise resolve from the response alone.

Each resource item in the `resources` array carries:

| Field | Description |
| --- | --- |
| `cloud_resource_uid` | Canonical shared identity key |
| `provider` | Normalized provider token: `aws`, `gcp`, or `azure` |
| `resource_type` | Provider resource type string |
| `management_origin` | Strongest contributing evidence layer |
| `scope_id` | Canonical ingestion scope id of the resource's admitting collector partition |
| `generation_id` | Evidence generation that produced this row |
| `source_state` | Provider-neutral truth label derived from `management_origin` |
| `evidence` | Per-layer boolean flags: `declared`, `applied`, `observed` |
| `tag_value_fingerprints` | Optional keyed non-reversible tag value markers; raw tag values are never returned |
| `identity_policy_evidence` | Optional bounded Azure identity-policy rows (keyed fingerprints only; no raw principal GUIDs or assignment scopes) |
| `resource_change_freshness` | Optional sanitized Azure Resource Graph change rows (no raw provider targets or actor ids) |
| `attributes` | Optional bounded provider-specific attributes. See below for what each provider surfaces. |

The `attributes` field is present only when the provider source fact carried
attribute evidence the route is allowed to surface, and its contract differs
by provider:

- **GCP** surfaces its typed-depth payload as a bounded redaction-safe
  passthrough (e.g. `table_type`, `schema_field_count`, `kms_key_name`,
  `clustering_fields` for BigQuery tables; `routing_mode`,
  `auto_create_subnetworks`, `mtu`, `subnetwork_count` for VPC networks).
  Values are redaction-safe scalars and string-arrays.
- **AWS** surfaces a CLOSED image/version allowlist only, scoped to the
  strongest deployed-code signals the collector already observes:
  `task_definition_arn`, `image_uri`, `resolved_image_uri`, `code_sha256`,
  `package_type`, `version`, and a `containers` array (from ECS running tasks)
  reduced per element to `{image, image_digest}`. `package_type` is the bounded
  Lambda packaging discriminator (`Zip`/`Image`). Every other AWS attribute key
  (for example `cluster_arn`, `role_arn`, `kms_key_arn`, `network_interfaces`,
  `environment`, `vpc_config`, or a container's `name`/`runtime_id`) is
  dropped before the route ever sees it.
- **Azure** uses the same closed-allowlist mechanism as AWS, but the
  allowlist is currently empty: the `azure_cloud_resource` fact this route
  reads carries no image or version key today (Azure's runtime image
  evidence is emitted as a separate `azure_image_reference` fact kind not
  yet wired into this admission path), so no Azure resource surfaces an
  `attributes` field yet. Every raw Azure attribute (`arm_resource_id`,
  `subscription_id`, `resource_group`, `tenant_id`, `tags`, the redacted
  `extension` object, ...) is dropped.

`attributes` surfaces deployed-code identity evidence — image references
(`image_uri`, `resolved_image_uri`, the ECS `containers[].image`) and the
owning `task_definition_arn` — which necessarily name the image, registry, and
repository for the caller's own resources. This route is account-scoped (it is
filtered by `scope_id`/`account_id`), so those identifiers are the operator's
own, not another tenant's. What is never present is any credential, secret, or
non-image infrastructure locator: `cluster_arn`, `role_arn`, `kms_key_arn`,
`network_interfaces`, `environment`, `vpc_config`, a container's
`name`/`runtime_id`, or the Azure `arm_resource_id`/`subscription_id`/`tags`
bag are all dropped before the route ever sees them.

### Lambda `code_sha256` is display-only, not correlated (bounded gap)

For a **zip-packaged** Lambda function (`attributes.package_type` == `Zip`)
that carries a `code_sha256`, the resource row additionally carries a bounded
`code_sha256_correlation` object that states, explicitly, that the code hash is
**not** correlated to any CI or package hash:

```json
"code_sha256_correlation": {
  "status": "uncorrelated",
  "truth_basis": "display_only_evidence",
  "unsupported_reason": "zip_code_sha256_no_ci_counterpart"
}
```

A Lambda `code_sha256` is `base64(SHA256(the exact deployment .zip))` computed
by AWS over the uploaded package bytes. Eshu collects **no** hash that covers
those bytes, so a byte-equal join cannot exist and no join is attempted:

- The GitHub Actions `artifact_digest` hashes GitHub's **own re-zipped**
  archive (and Eshu consumes it as a container-image digest), not the Lambda
  package.
- Package-registry hashes are of published tarballs/wheels/module zips, not the
  Lambda deployment zip.
- OCI image digests are of container image manifests.

The `code_sha256` is therefore surfaced only as **display-only evidence**; the
limitation is stated programmatically on the row rather than left silent. This
field is **not** present for an **image-packaged** Lambda
(`package_type` == `Image`): its deployment code is the container image, which
*is* correlated to the OCI `ContainerImage` through `image_uri` /
`resolved_image_uri`.

No-Regression Evidence: issue #5454 adds one bounded scalar (`package_type`) to
the closed `awsCloudInventoryAttributeAllowlist` and one O(1) provider +
resource_type gated map-key check per already-fetched readback row
(`cloudInventoryCodeCorrelationLabel`); it introduces no new query, SQL, Cypher,
transaction, or per-row I/O, so the cloud-inventory readback wall-time and
handler duration are unchanged (verified by the `go test ./internal/query`
readback suite on the same corpus/fixtures).

No-Observability-Change: #5454 adds no new metric, span, log line, audit table,
or schema; the `code_sha256_correlation` limitation is a content-free label on
an existing read surface, covered by the existing cloud-inventory query span.

## Cloud Resource Graph Paging

`GET /api/v0/cloud/resources` returns a bounded browse page from the authoritative CloudResource graph. Optional `provider`, `resource_type`, `region`, and
`account_id` filters are applied before paging. Continue a truncated page by
passing both `next_cursor.after_resource_type` and `next_cursor.after_id`;
sending only one cursor field returns HTTP 400.

The route first selects a current, authorized `limit+1` identity page from the
Postgres graph-owner ledger, then hydrates only the returned `uid` values from
the graph. Scoped-token grants are evaluated before the page limit. An empty
grant returns an empty page without reading either backend, and graph/ledger
disagreement fails closed rather than serving partial data.

The response remains ordered by `resource_type`, then `id`, and includes
`resources`, `count`, `limit`, `truncated`, the applied `scope`, and `next_cursor`
only when another page exists. `local_lightweight` returns `unsupported_capability`.
