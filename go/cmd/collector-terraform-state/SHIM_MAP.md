# Internal → Public Shim Map (`#8868`, analysis artifact)

This is a **design map, not an implementation**. It records how each
in-tree seam becomes a public SDK exchange at extraction, so the `#6710`
template repository and the `#4047` parity gate share one
interpretation. Nothing here changes runtime behavior.

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no
in-tree removal. Cutover stays gated by `#4047` with production
acceptance NOT_CHECKED.

Scope of this PREP (edits): `go/cmd/collector-terraform-state/`
(`SHIM_MAP.md`, `PRODUCER_GRANT_DRAFT.md`, `ROLLBACK_DRAFT.md`).
Census references are read-only: `go/cmd/collector-terraform-state/`
(`config.go`, `service.go`, `main.go`,
`target_scope_source_factory.go`), `go/internal/collector/tfstateruntime/`
(`source.go`, `source_helpers.go`, `fact_spool.go`, `metrics.go`,
`too_large_warning.go`, `composite_capture_recorder.go`),
`go/internal/collector/terraformstate/` (discovery, parser, sources),
`go/internal/facts/cloud/terraform_state.go`, `go/internal/scope/tfstate.go`.
Out of scope: `sdk/go` (Codex `#8851`), all other collectors, org repos,
deployment.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll path for the live binary:
`ClaimedSource.NextClaimed` (`tfstateruntime/source.go:156`) is the only
live emission entry. `buildClaimedService` (`service.go:57`) wires the
claimed runner with `CollectorKind: scope.CollectorTerraformState`
(`"terraform_state"`), claim lease TTL, heartbeat interval,
`MaxAttempts`, and `newClaimID` (`service.go:133`, `tfstate-claim-`
namespace prefix with crypto-rand hex and monotonic fallback).
`buildCassetteService` (`service.go:33`) wires the credential-free
cassette replay path; no live credentials needed.

Admission to the claim path is `validate` (`source.go:183`): the source
factory must be set, the redaction key must be non-zero, `item.CollectorKind`
must equal `scope.CollectorTerraformState`, `item.SourceSystem` must equal
`"terraform_state"`, and `item.CurrentFencingToken` must be positive.
Claim-to-candidate binding is two-phase (`source_helpers.go`): the cheap
pre-open check `claimMatchesCandidate` (`source_helpers.go:101`) matches
`item.ScopeID` against the derived snapshot scope and, for planning-id
work items, the candidate planning ID; the post-open check
`claimMatchesCollected` (`source_helpers.go:116`) matches the resolved
generation ID. `usesCandidatePlanningID` (`source_helpers.go:135`)
disambiguates planning-id items from resolved-generation items.
Generation IDs are reused on retry, so retries are idempotent. Per-claim
target credentials resolve through `targetScopeSourceFactory`
(`target_scope_source_factory.go:40`): an explicit `target_scope_id`
binds exactly one allowlisted target scope; otherwise the single
matching allowlisted scope wins and zero or ambiguous matches fail
closed. Local-backend candidates validate against the same allowlist
without opening AWS clients.

| In-tree outcome | SDK `ResultState` (`sdk/go/collector/types.go`) | Host action |
| --- | --- | --- |
| parsed envelopes committed | `complete` | validate facts, complete claim |
| `state_too_large` / `state_missing` warning generation (`too_large_warning.go`) | `partial` + `Status{warning}` + `terraform_state_warning` facts | commit reachable evidence |
| S3 not-modified, prior generation reused (`source.go:240`, `Unchanged: true`) | `unchanged` | complete claim, no fact writes |
| empty generation (`ok=false`), nothing to write | `unchanged` | complete claim, no fact writes |
| transient transport / retryable provider status (`sourceError` build/open wrapper, `source_helpers.go:210`) | `retryable` | release claim, bounded retry |
| terminal (auth-denied at claim scope, lineage/serial identity mismatch `parser.go:263-275`, contract violation) | `terminal` | terminal-fail claim |
| cancellation (`ctx.Err`, `source.go:163`) | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`facts.StableID("TerraformStateFact", …)` in `parser.go:279-288`,
keyed on fact kind, stable key, scope id, generation id) and committed
generations are idempotent at the core boundary.

| In-tree field | SDK `Claim` (`sdk/go/collector/types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorTerraformState`) | `collector_kind` |
| `item.CollectorInstanceID` vs `config.Instance.InstanceID` | `instance_id` (single enabled claim-capable instance selected in `config.go:266`) |
| `item.ScopeID` | `scope` (snapshot scope id, `state_snapshot:<backend>:<locator_hash>`) |
| `item.GenerationID` / `item.SourceRunID` (planning or resolved form) | `generation_id` (reused on retry) |
| `item.CurrentFencingToken` | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target-scope scan config (credentials, allowlists, lock table) | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the
producer, decides lease expiry and supersession. The producer must treat
a claim it did not `.Claim()` as foreign and never write for it.

## 2. The `terraform_state_*` kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per emitted kind, each `schema_versions: ["1.0.0"]`
(`terraform_state.go:54-74` in `facts/cloud`,
`TerraformStateFactKinds()`, eight family kinds):

```json
[
  {"kind": "terraform_state_snapshot", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_resource", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_output", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_module", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_provider_binding", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_tag_observation", "schema_versions": ["1.0.0"]},
  {"kind": "terraform_state_warning", "schema_versions": ["1.0.0"]}
]
```

Seven kinds, not eight: `terraform_state_candidate` is family-declared
but emitted by the git collector under `core_owned` authority
(`terraform_state.go:117`, producer `git-core`). It is NOT in this
grant; the extracted `terraform-state` producer must never declare or
emit it. The family declaration (`terraform_state.go:93-119`) is already
cutover-shaped: producer `collector-terraform-state`, authority mode
`core_issued_grant`, the seven kinds above, planned repository
`eshu-hq/eshu-collector-terraform-state`. The corresponding SDK `Fact`
mapping is `stable_key` ← `kind + ":" + stableKey`
(`parser.go:277-288`), `source_ref.scope_id` ← scope `ScopeID`,
`source_ref.generation_id` ← committed generation,
`collector_kind` ← `"terraform_state"`, fencing token ← claim token,
`source_confidence` ← observed; locator-bearing payload fields stay
redacted (`redact.String(…, s.RedactionKey)` in
`too_large_warning.go:108-113`, emitted as marker/reason/source maps)
and metric labels carry only `safe_locator_hash`, never raw locators.

## 3. `targetJSON` operator shape (unchanged by extraction)

The collector-instance `Configuration` JSON (`config.go:61-69`,
`terraformstate/config.go:12-55`) keys:

- `aws`: `role_arn`, `external_id`, `dynamodb_table`,
  `dynamodb_lock_table` (legacy alias).
- `target_scopes[]`: `target_scope_id`, `provider`, `deployment_mode`,
  `credential_mode` (`default` | `central_assume_role` |
  `local_workload_identity`), `role_arn`, `external_id`,
  `allowed_regions`, `allowed_backends`, `redaction_policy_ref`.
- `allow_empty_successor`: explicit operator attestation for a
  legitimately emptied state (snapshot only, zero body facts); defaults
  false. Also settable via `ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR`.
- `discovery`: `graph`, `seeds[]` (exact `kind`/`path`/`bucket`/`key`/
  `region`/`version_id`/`dynamodb_table` plus `target_scope_id`),
  `local_repos[]`, `backend_filters[]`, `local_state_candidates`
  (`mode`, `approved[]`, `ignored[]`).

Startup environment (`config.go:114-204`): `ESHU_COLLECTOR_INSTANCES_JSON`
(selects the single enabled claim-capable `terraform_state` instance;
ambiguous multi-instance without `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID`
fails closed), poll interval, claim lease TTL, heartbeat interval
(heartbeat >= lease fails closed), `ESHU_TFSTATE_REDACTION_KEY`
(required, never hardcoded), `ESHU_TFSTATE_REDACTION_RULESET_VERSION`
(required; blank version fails closed at startup),
`ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` (optional, defaults to the
`defaultRedactionSensitiveKeys` AWS/GCP/TLS list in `config.go:27-42`),
`ESHU_TERRAFORM_SCHEMA_DIR` (packaged provider-schema override;
startup fails when neither override nor embedded bundle yields an
attribute), `ESHU_TFSTATE_SOURCE_MAX_BYTES` (must not be negative),
`ESHU_TFSTATE_COLLECTOR_OWNER_ID`. Role ARNs travel by reference (ARN
strings in configuration, SDK-resolved per region); locator text is
redacted out of errors (`sourceFailure`, `source_helpers.go:210`), and
raw state bytes, bucket/key pairs, and secret values never reach logs,
spans, metrics, or docs (`AGENTS.md` invariants).

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `tfstate.collector.claim.process` | claimed-service runner |
| span | `tfstate.discovery.resolve` | discovery resolver |
| span | `tfstate.source.open` | `source.go:387` (`openSource`) |
| span | `tfstate.parser.stream` | `source.go:358` (`parseCandidate`) |
| span | `tfstate.fact.emit_batch` | fact emit path |
| span | `tfstate.coordinator.complete` | coordinator completion |
| counter | `eshu_dp_tfstate_snapshots_observed_total` | `metrics.go:17` (`backend_kind`, `result`: `parsed`/`not_modified`/`state_too_large`/`state_missing`/`error`) |
| counter | `eshu_dp_tfstate_resources_emitted_total` | `metrics.go:34` (`backend_kind`) |
| counter | `eshu_dp_tfstate_outputs_emitted_total` | `metrics.go:60` (`backend_kind`, `safe_locator_hash`) |
| counter | `eshu_dp_tfstate_modules_emitted_total` | `metrics.go:75` (same labels) |
| counter | `eshu_dp_tfstate_warnings_emitted_total` | `metrics.go:91` (`backend_kind`, `safe_locator_hash`, `warning_kind`) |
| counter | `eshu_dp_tfstate_redactions_applied_total` | `metrics.go:116` (`reason`) |
| counter | `eshu_dp_tfstate_s3_conditional_get_not_modified_total` | `metrics.go:131` (S3 only) |
| counter | `eshu_dp_drift_schema_unknown_composite_total` | composite-capture recorder (`resource_type`, closed `reason` enum; high-cardinality `attribute_key` stays in first-occurrence slog only) |
| histogram | `eshu_dp_tfstate_snapshot_bytes` | `metrics.go:31` (1 KiB–100 MiB) |
| histogram | `eshu_dp_tfstate_parse_duration_seconds` | `metrics.go:140` (0.001–10 s) |
| gauge | `eshu_dp_tfstate_schema_resolver_entries` | startup schema-bundle footprint (`service.go:78`) |
| histogram | `eshu_dp_tfstate_claim_wait_seconds` | core-owned claim-wait recording (`claimed_service.go:225`) |

Raw repository names, bucket/key locators, token values, and state
payloads must never appear in metric labels. The host additionally
records grant decisions per emission (`#6709`).

No-Observability-Change: this PREP adds no signals; the existing
`eshu_dp_tfstate_*` counters, snapshot-bytes and parse-duration
histograms, schema-resolver gauge, composite-capture counter, and the
six spans already cover the claimed path. The external producer must
preserve them verbatim.

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary).
   External fails closed on missing/expired/revoked grant at install,
   activation, and every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore`
   directly (`service.go:84`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat
   (`service.go:115-131`). External receives a bounded `Claim` and
   returns a `Result`; transitions stay `eshu-workflow`.
4. **Shared kinds**: the `terraform_state_*` kinds are also the contract
   of the fact family, not of sibling collectors — but the grant's
   producer identity + scope (`terraform_state` instance scope) is what
   separates this producer's authority from the git collector's
   `terraform_state_candidate` authority, not the kind family name.
5. **No durable collector state**: unlike watermark collectors, tfstate
   keeps no per-process durable state — prior snapshots, backend facts,
   and git readiness stay core-owned
   (`service.go:93-99`, `postgres.TerraformState*Reader`). The external
   producer keeps no durable cursor; re-resolution after restart is
   core-side until the extracted release proves its own equivalent.
6. **Startup-required redaction bundle**: the redaction key, versioned
   `redact.RuleSet`, and packaged provider-schema resolver are
   startup-fatal when absent (`config.go:229-264`). Cutover must
   provision all three to the external producer; without the resolver
   every scalar redacts and state rows silently orphan from their cloud
   resources.
7. **Trusted-deletion attestation**: `allow_empty_successor` is operator
   input, not producer state. Only the attested legitimate-empty shape
   (snapshot only, zero body facts) bypasses the store's
   successor-emptiness backstop (`source_helpers.go:44-65`); failure
   shapes (missing, over-ceiling, identity-mismatched) never attest,
   even with the flag set.
8. **Fact spool**: the temp-gob `factSpool` (`fact_spool.go`, 500-envelope
   stream buffer) is an in-tree memory-safety detail, not a contract.
   External returns facts; core streams them.
