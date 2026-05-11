# Design: Terraform Config-vs-State Drift Correlation

**Date:** 2026-05-10
**Status:** Design only — no implementation in this artifact
**Authors:** eshu-platform
**Tracks:** issue #43 (tfstate/E Correlation DSL Integration), partial coverage
**Related ADR:** `docs/docs/adrs/2026-04-20-terraform-state-collector.md`
**Related plans:** `docs/superpowers/plans/2026-04-20-terraform-state-collector-architecture-workflow.md`

This is a design doc. It does not modify Go code, tests, or runtime behavior.
It defines the rule pack contract, identity model, fixture plan, telemetry
labels, and verification gate that a future implementer will follow when the
upstream prerequisites land.

---

## 1. Context

Issue #43 ("tfstate/E Correlation DSL Integration") originally bundled three
deliverables:

1. A `terraform_state` rule pack that consumes the new state facts.
2. Drift detection between Terraform config (HCL parser output) and
   Terraform state facts (collector output).
3. State-to-cloud ARN joins that anchor state-observed resources to cloud
   inventory.

This document scopes only deliverable (2). Deliverable (3) is blocked on the
AWS scanner facts gated by issue #48 (architecture gate); the explicit deferral
is recorded in section 3. Deliverable (1) is a sibling rule pack and is owned
by a separate slice; it is not designed here.

Why split now: the Terraform-state collector emits the facts the drift design
needs (`go/internal/facts/tfstate.go:13-17`), the parser already buckets
config-side facts (`go/internal/parser/hcl/parser.go:46-72`), and the
correlation DSL ships a stable schema (`go/internal/correlation/rules/schema.go:43-57`).
There is no AWS dependency in either side of the comparison. ARN joins, by
contrast, require AWS scanner facts that issue #48 has not yet ratified, and
designing rule details against a moving target would be wasted work.

Dependency graph for this design becoming implementation work:

- **Upstream prerequisites (must land first):**
  - The `terraform_state` rule pack (issue #43, separate slice) — provides the
    `terraform_state` evidence-type vocabulary that drift rules consume.
  - Reducer-side admission of state facts and config facts onto comparable
    correlation candidates (Agent A's resolver work) — drift rules consume
    candidates produced by the engine, not raw evidence atoms.
  - Drift telemetry contract registration (Agent B's status/telemetry slice) —
    the metric names listed in section 7 are #43-required but not yet declared
    in `go/internal/telemetry/instruments.go`.
- **Non-blocking:** ARN-join work (#48) can proceed in parallel once unblocked
  and reuses the same registry pattern.

---

## 2. In Scope

This design covers drift detection between two fact families originating from
the same logical Terraform repository at compatible points in time.

### 2.1 Source-of-truth inputs

Config side, produced by the HCL parser into `parsed_file_data` buckets, see
`go/internal/parser/hcl/parser.go:91-187`:

- `terraform_resources` — one row per `resource "<type>" "<name>" {}` block,
  optionally with `for_each` / `count` expansion metadata.
- `terraform_modules` — one row per `module "<name>" {}` block.
- `terraform_backends` — one row per `terraform { backend "<kind>" {} }` block,
  used here only to scope the comparison to a known state locator.

State side, emitted as facts by the Terraform-state collector
(`go/internal/facts/tfstate.go:13-17`):

- `terraform_state_resource` — one resource instance from a state snapshot.
- `terraform_state_output` — one output from a state snapshot.
- `terraform_state_module` — one module entry from a state snapshot.

Tag, provider-binding, and warning facts (`go/internal/facts/tfstate.go:20-25`)
are out of scope for the first slice; they may inform later attribute-drift
extensions.

### 2.2 Identity contract

Drift detection requires a stable address that exists on both sides. The
authoritative address is the Terraform resource address:

```
[<module_path>.]<type>.<name>[<index_suffix>]
```

Where:

- `module_path` is the dotted module call chain (`module.foo.module.bar`),
  empty for the root module.
- `type` is the resource type (e.g. `aws_s3_bucket`).
- `name` is the HCL block label.
- `index_suffix` is `["<key>"]` for `for_each`, `[<n>]` for `count`, or empty
  for singletons.

Config side derives this address from HCL block labels and `for_each` / `count`
metadata in `terraform_resources` rows. State side derives it from the state
file's per-resource `module`, `type`, `name`, and instance `index_key` fields,
which the collector already normalizes when it emits
`terraform_state_resource`. Both sides must canonicalize to lowercase type and
name and to a deterministic index encoding before comparison.

### 2.3 Scope boundary

Drift is computed per `state_snapshot` scope, joined to one Terraform config
fact set drawn from a deterministic point in repo history. The state-snapshot
scope is defined in `go/internal/scope/tfstate.go:11-42`; the join key is the
backend identity carried in the scope metadata (`backend_kind`, `locator_hash`).

The link from a state snapshot to a config snapshot is currently mediated by
`terraform_backends` parser facts: a parsed `terraform { backend "s3" { ... } }`
block produces a backend identity that hashes to the same `locator_hash` the
collector uses, per the collector ADR (`docs/docs/adrs/2026-04-20-terraform-state-collector.md`
lines 374, 438, 473). The drift rule pack assumes this join exists upstream and
delivers a single correlation candidate carrying both config and state evidence
for one backend identity. Building that joiner is upstream work (Agent A's
resolver); the rule pack does not own the join, only the comparison.

If the upstream joiner has not produced a candidate that contains both sides,
the rule pack must reject the candidate as `structural_mismatch` rather than
silently skip it. See the open question in section 9 about which exact repo
commit feeds the comparison.

---

## 3. Out of Scope (Explicit Deferral)

State-to-cloud ARN joins are not designed here. Issue #43 lists this work and
the ADR points at the package path
`go/internal/correlation/rules/state_to_cloud_arn/`
(`docs/docs/adrs/2026-04-20-terraform-state-collector.md:1031`). The deferral
reasons:

- **Blocker:** the join requires cloud-side ARN, account, region, and resource
  kind facts that only the AWS scanner can emit. Issue #48 is the architecture
  gate for the AWS scanner; until it closes, the cloud-side fact contract is
  not stable.
- **Risk of premature design:** picking specific Terraform-state attributes per
  resource type (`aws_s3_bucket.arn`, `aws_iam_role.arn`,
  `aws_lambda_function.arn`) is straightforward, but the matching cloud-side
  evidence type, scope, and confidence model are not yet ratified. Designing
  field-by-field rules now would force rework when #48 closes.

What this design does record for the future ARN-join slice:

- The forward-compatible signature an ARN-join candidate must carry: `arn`,
  `aws_account_id`, `aws_region`, `cloud_resource_kind`, plus the same
  Terraform resource address used here so a single correlation key spans
  config, state, and cloud sides.
- The proposed package location: `go/internal/correlation/rules/state_to_cloud_arn/`,
  registered through the same `FirstPartyRulePacks()` slice used today for the
  drift pack (see section 4).
- The forward seam: per-resource-type ARN extractors should live next to the
  state collector's parser, not inside the rule pack, so the pack consumes
  pre-extracted `arn` evidence atoms rather than re-walking state attributes.

The drift pack designed here MUST NOT take any dependency on cloud-side facts.

---

## 4. Rule Shape

### 4.1 DSL primitives available today

The correlation DSL is declarative metadata, evaluated by a deterministic
engine. The available primitives (`go/internal/correlation/rules/schema.go:9-17`)
are:

- `RuleKindExtractKey` — declares that a rule extracts a correlation key from
  candidate evidence.
- `RuleKindMatch` — declares an evidence-matching step with a bounded
  `MaxMatches`.
- `RuleKindAdmit` — declares the admission step (gated by
  `MinAdmissionConfidence` and `RequiredEvidence`).
- `RuleKindDerive` — declares a derivation step that produces downstream
  attributes for the candidate.
- `RuleKindExplain` — declares the provenance/explanation hook the explain
  package consumes.

A `RulePack` (`go/internal/correlation/rules/schema.go:52-57`) bundles
`Name`, `MinAdmissionConfidence`, `RequiredEvidence`, and an ordered slice of
`Rule` entries. The engine sorts rules by `Priority` then `Name` and applies
admission deterministically (`go/internal/correlation/engine/engine.go:30-90`).
Admission requires both confidence and structural-evidence gates to pass
(`go/internal/correlation/admission/admission.go:30-42`).

The DSL does **not** today expose a primitive for "compare two evidence sets
and emit a delta". Drift kinds (section 5) must therefore be expressed as
derivations applied to candidates that already carry both config and state
evidence, with the comparison logic implemented in the rule-pack helper Go
file alongside the `RulePack` declaration. The terragrunt and terraform-config
packs follow the same shape: they are thin metadata declarations
(`go/internal/correlation/rules/terragrunt_rules.go:5-27`,
`go/internal/correlation/rules/terraform_config_rules.go:4-26`) that the
engine evaluates, with semantics carried in the helper code that builds the
candidates.

### 4.2 Package layout

New code lives under `go/internal/correlation/rules/terraform_config_state_drift/`.
The package contains:

- A `RulePack()` constructor analogous to `TerraformConfigRulePack()` and
  `TerragruntRulePack()`.
- Helper functions that walk admitted candidate evidence to classify each
  drift kind and emit the metric counters in section 7.
- Versioned fixtures under `testdata/`.

Registration adds the new pack to `FirstPartyRulePacks()` in
`go/internal/correlation/rules/container_rulepacks.go:21-35`. That is the
single registry today; no plugin loader exists. (See section 9 — the registry
is by direct slice append.)

### 4.3 Pseudocode sketch (not compilable Go)

Rule-pack declaration:

```text
RulePack{
    Name: "terraform_config_state_drift",
    MinAdmissionConfidence: 0.85,
    RequiredEvidence: [
        Requirement{ Name: "config-resource-address",
                     MinCount: 1,
                     MatchAll: [
                         Selector{Field: EvidenceType, Value: "terraform_config_resource"},
                         Selector{Field: Key,          Value: "resource_address"},
                     ]},
        Requirement{ Name: "state-resource-address",
                     MinCount: 1,
                     MatchAll: [
                         Selector{Field: EvidenceType, Value: "terraform_state_resource"},
                         Selector{Field: Key,          Value: "resource_address"},
                     ]},
    ],
    Rules: [
        Rule{Name: "extract-resource-address-key", Kind: ExtractKey, Priority: 10},
        Rule{Name: "match-config-against-state",   Kind: Match,      Priority: 20, MaxMatches: 1},
        Rule{Name: "derive-drift-classification",  Kind: Derive,     Priority: 30},
        Rule{Name: "admit-drift-evidence",         Kind: Admit,      Priority: 40},
        Rule{Name: "explain-drift-classification", Kind: Explain,    Priority: 50},
    ],
}
```

Classification helper (called inside the derive step):

```text
classify(candidate):
    config = evidence_by(EvidenceType="terraform_config_resource")
    state  = evidence_by(EvidenceType="terraform_state_resource")
    if state and not config:
        emit drift{kind=added_in_state,    address=state.address}
    if config and not state:
        emit drift{kind=added_in_config,   address=config.address}
    if config and state and state.generation_marked_removed:
        emit drift{kind=removed_from_state, address=state.address}
    if config and state and config.absent_in_latest_repo_commit:
        emit drift{kind=removed_from_config, address=config.address}
    if config and state and attribute_diff(config, state, allowlist):
        emit drift{kind=attribute_drift,   address=config.address,
                   attributes=changed_keys}
```

The `attribute_diff` helper walks an explicit allowlist of attributes per
resource type. Unknown / computed values on the config side are treated as
"no signal" and never raise drift.

---

## 5. Drift Kinds

All five drift kinds map to the `drift_kind` label of
`eshu_dp_correlation_drift_detected_total` named in issue #43.

| Drift kind | Definition |
| --- | --- |
| `added_in_state` | A `terraform_state_resource` exists at address `A`; no `terraform_config_resource` evidence at `A` in the joined config snapshot. |
| `added_in_config` | A `terraform_config_resource` exists at address `A`; no `terraform_state_resource` evidence at `A` in the joined state snapshot. |
| `attribute_drift` | Both sides carry address `A` and at least one allowlisted attribute differs between config and state. |
| `removed_from_state` | Address `A` was present in a prior state generation (per the snapshot's lineage chain) but is absent from the current generation while config still declares it. |
| `removed_from_config` | Address `A` is present in the current state generation; the latest config snapshot for the joined repo no longer contains a matching block. |

Attribute selection for `attribute_drift`: the first slice limits comparison to
deterministic, operator-meaningful attributes — `tags`, `versioning`,
`encryption`, `acl`, `policy_arn`, `runtime`, `handler`, `memory_size`. The
allowlist lives in code, not in the DSL declaration, because the DSL has no
field-set primitive. Computed / unknown values surface as the literal string
the parser emits (e.g. an HCL expression token) and are never treated as a
mismatch against a concrete state value; this is the correctness rule that
prevents false positives from `data` references and `for_each` keys.

`removed_from_state` requires reading the prior generation in the same
state-snapshot lineage. The state collector already tracks lineage and serial
(`go/internal/scope/tfstate.go:46-74`); the rule pack must accept the prior
generation's resource set as part of its candidate evidence rather than
querying the graph directly. Producing that prior-generation evidence is
upstream work on the resolver (open question in section 9).

---

## 6. Fixture Plan

`eshu-correlation-truth` requires positive, negative, and ambiguous cases for
every drift kind. Fixtures live versioned under
`go/internal/correlation/rules/terraform_config_state_drift/testdata/`,
organized one subdirectory per drift kind.

### 6.1 `added_in_state`

- Positive: state contains `aws_s3_bucket.logs`; config has only
  `aws_s3_bucket.app`. Expected drift `added_in_state` for `logs`.
- Negative: state and config both contain `aws_s3_bucket.logs` with matching
  attributes. No drift.
- Ambiguous: state contains a resource imported via `terraform import` with no
  matching config block. Classifier emits `added_in_state`; the policy note
  documents this is operator-actionable, not a bug.

### 6.2 `added_in_config`

- Positive: config declares `aws_iam_role.svc`; state has no entry. Drift
  `added_in_config`.
- Negative: config and state both declare `aws_iam_role.svc`. No drift.
- Ambiguous: a `for_each` block in config produced a key set that the state
  has not yet been refreshed against (e.g. config added `roles["new"]`
  yesterday, state was last refreshed last week). Classifier emits
  `added_in_config`; documented as expected pre-apply state.

### 6.3 `attribute_drift`

- Positive: both sides declare `aws_s3_bucket.logs`; config sets
  `versioning.enabled = true`, state observes `false`. Drift on
  `versioning.enabled`.
- Negative: both sides declare matching `versioning.enabled = true`. No drift.
- Ambiguous: config has `tags = local.common_tags` (unresolved expression),
  state observes a concrete map. Classifier records "no signal" and does not
  emit drift on `tags`.

### 6.4 `removed_from_state`

- Positive: prior state generation contained `aws_lambda_function.worker`;
  current generation does not; config still declares it. Drift.
- Negative: resource is missing from prior and current; config never declared
  it. No drift.
- Ambiguous: prior generation had a duplicate-address bug that landed two
  instances of the same address (lineage rotation in flight). Classifier emits
  `lineage_rotation` warning rather than `removed_from_state`.

### 6.5 `removed_from_config`

- Positive: state contains `aws_iam_policy.legacy`; latest config no longer
  has the block. Drift.
- Negative: both sides agree the resource is gone. No drift.
- Ambiguous: a resource was renamed via `moved {}` block; the new address is
  in config, the old address is in state. Classifier emits
  `removed_from_config` for the old address and `added_in_config` for the new
  address; the policy note flags this as a known "moved-block transient" until
  the next state refresh.

---

## 7. Telemetry

Issue #43 requires two counters. Both are new and must be added to
`go/internal/telemetry/instruments.go` and `contract.go` in the implementation
slice.

| Metric | Type | Labels |
| --- | --- | --- |
| `eshu_dp_correlation_rule_matches_total` | counter | `pack`, `rule` |
| `eshu_dp_correlation_drift_detected_total` | counter | `pack`, `rule`, `drift_kind` |

Cardinality:

- `pack` is a frozen string from the rule-pack registry; for this design
  always `terraform_config_state_drift`.
- `rule` is the `Rule.Name` from the pack declaration; bounded by the rule
  count (5 in the sketch above).
- `drift_kind` is the closed enum in section 5: `added_in_state`,
  `added_in_config`, `attribute_drift`, `removed_from_state`,
  `removed_from_config`. Five values, no free-form input.

High-cardinality concern: resource address, attribute name, and module path
must NOT appear as metric labels. They go in span attributes and structured
logs. This follows the existing telemetry contract in
`CLAUDE.md` ("High-cardinality values such as file paths and fact IDs belong
in spans or logs, not metric labels").

The existing correlation summary type
(`go/internal/correlation/observability.go:9-15`) reports admission counters
but has no drift counter slot. The implementer should extend the summary or
emit drift via the global metrics registry rather than mutating the summary
shape, to avoid breaking existing consumers.

---

## 8. Verification Commands

The implementer should run, in this order, scoped to the new package and the
correlation tree:

```bash
cd go && go test ./internal/correlation/rules/terraform_config_state_drift/... -count=1
cd go && go test ./internal/correlation/... -count=1
cd go && go test ./internal/telemetry/... -count=1
cd go && golangci-lint run ./internal/correlation/...
scripts/verify-doc-claims.sh go/internal/correlation/rules/terraform_config_state_drift
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Acceptance evidence the implementer must cite when they declare ready:

- All five drift kinds have positive, negative, and ambiguous fixtures passing.
- `eshu_dp_correlation_drift_detected_total` and
  `eshu_dp_correlation_rule_matches_total` increment under fixture runs.
- Computed / unknown HCL values produce zero drift in the relevant negative
  fixtures (proves no false positives on `local.*` and `data.*` expressions).
- `FirstPartyRulePacks()` includes `terraform_config_state_drift` and the
  registry test in
  `go/internal/correlation/rules/container_rulepacks_test.go` is updated to
  cover the new entry.

---

## 9. Open Questions

These are the design points this author could not resolve from the codebase
alone. They are explicit asks for human input before implementation begins.

1. **Config-snapshot identity:** how does the upstream resolver pick the
   "matching commit" of the config repo for a given `state_snapshot`? The
   ADR's join key is `(backend_kind, locator_hash)` keyed off the
   `terraform_backends` parser fact, but a single backend can be referenced
   from multiple historical commits of the same repo. Walking the
   relationships pipeline (`go/internal/relationships/resolver.go`,
   `evidence.go`) did not surface a current "latest-config-commit-for-backend"
   resolver; this may need a new resolver step before the rule pack is useful.
2. **Cross-scope candidate construction:** the DSL's `EvidenceAtom` carries a
   single `ScopeID` (`go/internal/correlation/model/types.go:38`). A drift
   candidate needs evidence atoms from the config scope (repo-snapshot scope)
   and the state scope (`state_snapshot` scope) on the same `Candidate`. The
   engine accepts that today (it does not enforce single-scope candidates),
   but no existing rule pack does it. Confirm this is intended and not an
   accidental side effect; if it is intended, document the cross-scope
   pattern in the rules `README.md`. If not, the engine needs a new primitive
   first.
3. **Rule-pack registration site:** `FirstPartyRulePacks()` at
   `go/internal/correlation/rules/container_rulepacks.go:21-35` is the only
   in-tree registry. Should the drift pack land in that flat list, or is a
   second list (drift / cross-cutting packs) preferred to keep semantic
   families separated? The reducer at
   `go/internal/reducer/deployable_unit_correlation.go:225-250` selects packs
   per workload candidate; drift candidates are not workload candidates, so
   the reducer does not call this pack today. Where does the engine get
   invoked for drift candidates? This blocks implementation.
4. **Prior-generation evidence delivery:** `removed_from_state` requires the
   prior state generation. Should the resolver attach prior-generation
   resource atoms to the same candidate, or should the rule pack issue a
   secondary lookup? The DSL has no lookup primitive, which argues for the
   resolver attaching the evidence.
5. **Attribute allowlist ownership:** the per-resource-type allowlist for
   `attribute_drift` is the most operator-meaningful policy decision in this
   design. Should it live in code (compiled allowlist) or in a versioned data
   file under `testdata/` peers, so operators can audit and tune it without
   recompilation?

---

## 10. Future Work (Post-#48)

Once issue #48 ratifies the AWS scanner fact contract, the same registration
pattern designed in section 4.2 applies to the deferred ARN-join rule pack at
`go/internal/correlation/rules/state_to_cloud_arn/`. That pack consumes
candidates carrying `terraform_state_resource` evidence plus AWS-scanner cloud
evidence keyed on the address signature in section 3, follows the same
positive / negative / ambiguous fixture discipline in section 6, and emits the
same `eshu_dp_correlation_rule_matches_total` counter with `pack` set to
`state_to_cloud_arn`. No drift counter applies to ARN joins; ARN matches are
identity, not drift.
