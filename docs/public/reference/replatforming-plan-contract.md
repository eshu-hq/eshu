# Replatforming Plan and Migration Packet Contract

The replatforming plan contract is the durable, versioned shape Eshu uses to
answer "what would it take to bring this scope under IaC management?" It is a
**read-only planning artifact**: it observes, compares, and plans, but it never
executes a migration.

The Go contract lives in
`go/internal/query/replatforming_plan_contract.go` (types) and
`go/internal/query/replatforming_plan_contract_validate.go` (invariants), with
tests in `go/internal/query/replatforming_plan_contract_test.go`. Per-item
evidence uses the
[source-state taxonomy](replatforming-source-state-taxonomy.md), and responses
follow the [Truth Label Protocol](truth-label-protocol.md).

This contract is defined **before** any serving route or MCP tool so that future
API/MCP work composes one consistent shape instead of stitching existing routes
together inconsistently. The serving route and tool land in a later slice.

## Version

`contract_version` pins the wire shape. Clients must check it. The current
version is `v1`.

## Requested Scope

A plan is anchored on exactly one primary `scope.kind`, with optional narrowing
fields:

| `scope.kind` | Meaning |
| --- | --- |
| `account` | One cloud account. |
| `region` | One account region. |
| `service` | One service. |
| `workload` | One deployable workload. |
| `repository` | One source repository. |
| `environment` | One environment. |
| `resource` | One stable resource identity. |

## Source Layers

Each migration packet item records the source layers it has or is missing, so a
coverage gap is never read as agreement:

- `declared_iac` — source-controlled IaC / Terraform configuration.
- `applied_terraform_state` — applied Terraform state.
- `observed_provider_runtime` — observed provider runtime evidence.
- `missing_evidence` — an explicitly absent layer.

Each layer carries a [source-state](replatforming-source-state-taxonomy.md)
value.

## Migration Packet Item

Each candidate resource carries: `item_id`, `provider`, `resource_type`,
`stable_id`, the provider-neutral `source_state`, the provider `management_status`
and `finding_kind`, `confidence`, `freshness`, the `safety_gate`, its
`source_layers`, `owner_candidates`, an optional `import_candidate`, and optional
`wave_id` / `blast_radius_group` membership.

### Owner Candidates

Ownership is never promoted to a single fabricated owner. When more than one
owner candidate of the same kind exists, every competing candidate must name its
`ambiguity_reasons`. An item whose `source_state` is `ambiguous` must not present
reason-free ownership. The contract validator rejects both violations.

### Import Candidates

An `import_candidate` is either `ready` (with a Terraform import block) or
`refused` (with `refusal_reasons` and no import block). The validator rejects a
ready candidate without a block, a refused candidate without reasons, and a
refused candidate that still carries a block.

## Waves and Blast Radius

`waves` order packet items for staged migration; `blast_radius_groups` group
items by dependency and risk so a wave's impact is explicit before any external
apply. Both reference items by `item_id`.

## Truth and Authority

`source_state.TruthLevel()` maps each item to the truth level it may carry: only
`exact` is authoritative, `derived`/`partial` are `derived`, and every uncertain,
unavailable, or gated state is `fallback`. A plan's rollup truth is the most
conservative item truth, so a plan never over-states authority. A serving route
additionally clamps to the capability's profile maximum; the contract test fails
if the declared `replatforming.plan.readiness` capability ever rises to `exact`,
which would let a route claim unsupported truth.

## Non-Goals

Every plan repeats its fixed non-goals:

- does not run Terraform or any migration
- does not import resources or mutate cloud state
- does not write user repositories
- does not promote provider observations to ownership truth without
  reducer-owned evidence

Sensitive material — raw tags, secrets, state locators, private URLs, provider
payloads, and credential names — stays out of the packet. The safety gate names
its redactions and refusals instead of silently omitting unsafe actions.
