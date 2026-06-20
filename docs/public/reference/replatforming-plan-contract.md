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

!!! note "Cloud coverage is AWS-first"
    AWS-side runtime drift and source-state evidence behind `compose_replatforming_plan`
    is `implemented` (production-promoted). The Azure and GCP equivalents are
    `gated`, not at parity: only `aws_resource_materialization` is promoted to a
    versioned, hashed conflict family today. State uses the canonical readiness
    lanes from
    [Collector And Reducer Readiness](collector-reducer-readiness.md#readiness-vocabulary);
    see the
    [cloud posture production-readiness table](../roadmap.md#cloud-posture-production-readiness)
    and [Supply-Chain Traceability](../supply-chain-traceability.md) for the
    promotion line.

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
apply. Both reference items by `item_id`. Each item also carries its `wave_id`
and `blast_radius_group` membership. The ordering is computed in
`go/internal/query/replatforming_waves.go` and is a **planning hint only**: it
never implies automatic apply and never fabricates a dependency the plan does not
already carry.

### Ordering signals

Ordering is derived only from evidence the composed plan already holds — never a
guessed dependency:

- **Dependency footprint** — the number of `dependency_paths` recorded on the
  item's finding, used as a downstream-dependent proxy. More paths mean a larger
  blast radius if the resource is imported, retired, renamed, or moved.
- **Missing evidence** — the number of explicitly missing-evidence entries,
  which raises blast-radius uncertainty without inventing dependents.
- **Safety gate / source state** — a `rejected` or `ambiguous` source state, or a
  safety gate that requires review, blocks an item from any earlier wave.
- **Import readiness** — only items with a `ready` import candidate are eligible
  for the earliest wave, because they have a concrete, safety-approved next step.

Output is deterministic: identical input yields identical waves and groups
regardless of item order, because every grouping iterates a fixed, sorted key
order. Item IDs within a wave or group are sorted.

### Waves

| `wave.id` | `order` | Holds |
| --- | --- | --- |
| `wave-1-early-safe` | 1 | Import-ready, low-blast-radius, non-gated items: safest early candidates. |
| `wave-2-review` | 2 | Non-gated items needing review: refused import, weaker evidence, or larger blast radius. |
| `wave-3-blocked` | 3 (always last) | Safety-gated, rejected, or ambiguously owned items: must wait for stronger evidence or a human safety decision. |

Empty waves are dropped, so a populated plan never carries a fabricated empty
stage and `order` stays contiguous starting at 1. Each wave carries a `rationale`.

### Blast-radius groups

Groups are emitted in ascending severity. `none`/`low`/`medium`/`high` are driven
by the dependency-plus-missing-evidence weight (`0`, `1–2`, `3–5`, `6+`).
`blocked` is independent of footprint: a safety-gated, rejected, or ambiguously
owned item is always `blocked`, because its evidence — not its size — blocks it.

### Response summaries

The serving route additionally returns bounded `wave_summaries` (per-wave
`item_count` in staging order) and `blast_radius_summaries` (per-group
`item_count` in ascending severity), so a consumer can triage staging without
walking every item.

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
