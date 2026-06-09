# Replatforming Source-State Taxonomy

Replatforming plans and migration packets describe many items — candidate
resources, owners, import candidates, and drift findings — that each carry a
different strength of evidence. The source-state taxonomy is the
**provider-neutral** vocabulary for that per-item evidence strength. It lets
API, MCP, and CLI clients compare AWS, GCP, and Azure items with one set of
states without flattening provider-specific source truth.

This taxonomy governs **per-item** state. The top-level response still follows
the [Truth Label Protocol](truth-label-protocol.md), and cloud collectors still
emit their own [provider-specific facts](multi-cloud-collector-contract.md).
Only the query-facing item state is normalized here; no provider fact or status
name changes.

The Go contract lives in
`go/internal/query/replatforming_source_state.go` and is covered by
`go/internal/query/replatforming_source_state_test.go`.

## States

| State | Meaning |
| --- | --- |
| `exact` | Provider evidence and reducer-owned canonical identity agree for the item. For AWS this is a resource managed by Terraform across cloud, state, and config. |
| `derived` | A deterministic correlation exists, but it is not direct, full provider-plus-config proof. |
| `partial` | The collector could read only part of the configured scope or content family for the item. |
| `ambiguous` | Multiple deterministic ownership signals conflict and must not be promoted to a single owner. |
| `stale` | The latest accepted evidence is older than the configured freshness window. |
| `unavailable` | The source was configured but unreachable, unauthorized, or rate-limited without current evidence. |
| `unsupported` | The provider, tier, API, resource family, or relationship type does not expose this evidence. |
| `unknown` | Coverage or permission gaps keep the item's evidence unproven. This is the fail-safe state for unrecognized input. |
| `rejected` | A safety gate rejected promoting the read-only finding into ownership truth or migration automation. |

`rejected` wins over the evidence-derived state: a safety-gated finding is never
presented as ready, regardless of how strong its underlying evidence is.

Reads must not convert `partial`, `stale`, `unavailable`, `unsupported`,
`ambiguous`, or `unknown` into silent fallback truth. Each is explicit evidence
state, not a degraded success.

## AWS Management Status Mapping

AWS IaC management statuses map deterministically into the taxonomy. The
mapping mirrors the existing AWS runtime-drift outcome semantics and adds no new
AWS fact names. Unrecognized or empty input maps to `unknown` so a future AWS
status cannot silently present as confident evidence.

| AWS management status | Taxonomy state |
| --- | --- |
| `managed_by_terraform` | `exact` |
| `terraform_state_only` | `derived` |
| `terraform_config_only` | `derived` |
| `cloud_only` | `derived` |
| `managed_by_other_iac` | `derived` |
| `ambiguous_management` | `ambiguous` |
| `stale_iac_candidate` | `stale` |
| `unknown_management` | `unknown` |
| *(any unrecognized status)* | `unknown` |

A finding whose safety gate rejected promotion resolves to `rejected`
regardless of its management status.

## Multi-Cloud Adoption

GCP and Azure adopt the taxonomy without renaming their provider-specific
facts. The six [multi-cloud collector contract](multi-cloud-collector-contract.md)
per-item states are already taxonomy members and map by identity:

| Multi-cloud query state | Taxonomy state |
| --- | --- |
| `exact` | `exact` |
| `derived` | `derived` |
| `partial` | `partial` |
| `stale` | `stale` |
| `unavailable` | `unavailable` |
| `unsupported` | `unsupported` |
| *(any unrecognized state)* | `unknown` |

Provider collectors keep emitting `gcp_*` and `azure_*` source facts; reducers
keep their provider-specific status names. The taxonomy is a query-time
normalization for comparison, not a collector or reducer schema change.

## Profile Gating

The provider-neutral `replatforming.plan.readiness` capability is declared in
the capability matrix fragment
`specs/capability-matrix/replatforming.v1.yaml`.
Lightweight local runtime cannot materialize the reducer-owned drift and IaC
evidence the readiness rollup needs, so that profile returns
`unsupported_capability` rather than a downgraded answer. The serving route and
MCP tool land in a later slice; this taxonomy and capability row fix the truth
contract and profile gating first.

## Non-Goals

- No Terraform execution, cloud mutation, or repository writes.
- No promotion of provider observations to ownership truth without
  reducer-owned evidence.
- No renaming or collapsing of provider-specific facts behind a generic state.
- No raw tags, secrets, state locators, private URLs, provider payloads, or
  credential names in the item state.
