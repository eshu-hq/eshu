# Terraform State Collector — Extraction Prep Census (`#8868`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-terraform-state/` and
`go/internal/collector/terraformstate/` plus the claim adapter
`go/internal/collector/tfstateruntime/`. Out of scope: `sdk/go` (Codex
`#8851`), the git collector's candidate emission, all other collectors, org
repos, deployment. Doc placement follows the `cicd-run` pattern: this census
lives next to the binary at `go/cmd/collector-terraform-state/`, alongside
the staged [SHIM_MAP](SHIM_MAP.md),
[PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md), and
[ROLLBACK_DRAFT](ROLLBACK_DRAFT.md) — not the `sbom` pattern — because the
claim/command wiring (§1 binary table) is the extraction seam and the drafts
already anchor there.

## 1. Current packages

### Binary: `go/cmd/collector-terraform-state/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:70,95` | entry point, `-mode=claimed-live\|cassette` | `parseArgs`, `run` |
| `config.go:114` | env config, `ESHU_COLLECTOR_INSTANCES_JSON` shape | `loadRuntimeConfig` |
| `config.go:266,301` | instance validation | `selectTerraformStateInstance`, `validateTerraformStateInstance` |
| `config.go:210,365,407` | AWS/target-scope/attestation parsing | `parseAllowEmptySuccessor`, `parseAWSCredentialConfig`, `parseAWSDynamoDBLockTable` |
| `config.go:254,327,353` | startup-fatal redaction/schema bundle | `loadSchemaResolver`, `loadRedactionRules`, `loadRedactionKey` |
| `service.go:33` | credential-free replay wiring | `buildCassetteService` |
| `service.go:57` | claimed wiring | `buildClaimedService` |
| `service.go:133` | claim ID mint | `newClaimID` (`tfstate-claim-` prefix) |
| `aws_s3.go:23` | read-only S3 adapter, exact key + `IfNoneMatch` | `newAWSS3ObjectClient`, `safeS3GetObjectError` |
| `aws_dynamodb.go:29` | read-only lock metadata, `Digest` only | `newAWSDynamoDBLockMetadataClient` |
| `target_scope_source_factory.go:40` | per-scope source factory + allowlists | `OpenSource`, `credentialsForCandidate`, `targetScopeAllowsCandidate` |
| `time.go:8` | clock helper | `timeZeroUTC` |
| `doc.go` | package contract | — |

No unclaimed poll mode exists: the binary only runs claimed-live or
cassette replay. Target secrets travel by reference (role ARNs in
configuration, SDK-resolved per region); locator text is redacted out of
errors (`sourceFailure`). Raw state bytes, bucket/key pairs, and secret
values never reach logs, spans, metrics, or docs (cmd `AGENTS.md`
invariants).

### Library: `go/internal/collector/terraformstate/`

| File | Role | Key symbol |
| --- | --- | --- |
| `parser.go:28,52` | streaming parse entry points (spool-backed `ParseStream`) | `Parse`, `ParseStream`, `parseState` |
| `parser.go:276` | envelope constructor (all 7 owned kinds) | `envelope` |
| `resources.go:183` | per-instance resource emission | `emitResourceInstance` |
| `outputs.go:94` | output emission, sensitive fail-closed | `emitOutput` |
| `modules.go:14` | per-resource module observation | `emitModuleObservation` |
| `providers.go:25` | provider binding emission | `emitProviderBinding` |
| `tags.go:119` | tag observation emission | `emitTagObservation` |
| `attributes.go:39` | attribute read + redaction classification | `readAttributeValues`, `classifyAttributes` |
| `composite_walker.go:23` | streaming nested walker, no `Unmarshal` | `readCompositeValue` |
| `anchors.go:21` | correlation anchors | `correlationAnchors` |
| `identity_join_keys.go:83` | join-key exemption from SchemaUnknown (#5870) | `classificationSchemaTrust` |
| `identity.go:115,137` | version-aware vs version-agnostic hashes | `LocatorHash`, `ScopeLocatorHash` |
| `snapshot_identity.go:23` | serial/lineage pre-read | `ReadSnapshotIdentity` |
| `candidate_identity.go:15` | pre-read planning identity | `CandidatePlanningID` |
| `warnings.go:66` | warning facts + composite-warning flush | `emitWarning`, `flushCompositeAttributeWarnings` |
| `warning_fact.go:31` | pre-parse source-level warnings | `NewWarningFact` |
| `warning_classification.go:36` | severity/actionability contract | `ClassifyWarning` |
| `uncovered_provider.go:41,95` | stale-bundle detector | `recordUncoveredResourceType`, `flushUncoveredProviderWarnings` |
| `empty_state.go:21` | legitimate-empty shape gate | `IsLegitimatelyEmptyBody` |
| `discovery.go:106` | exact-candidate resolution, never opens sources | `DiscoveryResolver.Resolve` |
| `discovery_types.go`, `discovery_backend_filter.go`, `local_candidates.go` | candidate/discovery shapes | `DiscoveryCandidate`, `DiscoveryConfig` |
| `config.go:59` | collector-instance discovery JSON | `ParseDiscoveryConfig` |
| `backend_config.go:85` | Git-observed backend evaluation | `EvaluateBackendConfig` |
| `backend_config_local.go:40` | local-backend candidate + Terraform default path (#5594) | `backendConfigLocalCandidate` |
| `backend_config_resolution.go` | var/local/interpolation resolution | `resolveBackendConfigAttribute` |
| `source_terragrunt.go:36` | Terragrunt→exact-backend translation | `TerragruntRemoteStateCandidate` |
| `source_local.go:33` | exact absolute-file source | `NewLocalStateSource` |
| `source_s3.go:76` | exact-object read-only source, conditional ETag | `NewS3StateSource` |
| `source_limit.go:20` | size-ceiling reader | `newSizeEnforcingReadCloser` |
| `schema_resolver.go:123` | packaged provider-schema bundle | `LoadPackagedSchemaResolver` |
| `sensitive_attributes.go:42` | hard-sensitive attributes | `isHardSensitiveStateAttribute` |
| `pagerduty_applied.go:49` | applied incident-routing evidence (§2 shared-kind note) | `emitAppliedIncidentRoutingEvidence` |
| `metrics.go:20` | discovery metrics | `NewDiscoveryMetrics` |
| `encode_helpers.go:6` | contract-payload merge | `mergeContractPayload` |
| `json_token.go`, `fact_stream.go` | streaming decoder helpers, spool + dedupe | `skipNested`, `newFactSpool` |
| `types.go:21,52,124` | source interfaces, parse options, recorder seam | `StateSource`, `ParseOptions`, `CompositeCaptureRecorder` |
| `doc.go` | package contract | — |

Every production file is listed. The parser never calls `json.Unmarshal`
on a full payload; composites unknown to the schema drop via `skipNested`
and surface through the `eshu_dp_drift_schema_unknown_composite_total`
counter.

### Runtime adapter: `go/internal/collector/tfstateruntime/`

| File | Role | Key symbol |
| --- | --- | --- |
| `source.go:156` | only live emission entry | `ClaimedSource.NextClaimed` |
| `source.go:183,202` | claim admission, per-candidate collection | `validate`, `collectCandidate` |
| `source.go:322,339` | identity pre-read, spool-backed parse | `readIdentity`, `parseCandidate` |
| `source_helpers.go:101,116` | two-phase claim↔candidate binding | `claimMatchesCandidate`, `claimMatchesCollected` |
| `source_helpers.go:44` | trusted-deletion attestation gate | `attestTrustedDeletion` |
| `fact_spool.go:35` | temp-gob fact stream (500-envelope buffer) | `newFactSpool` |
| `metrics.go` | per-locator emission counters | `recordResourceFacts` … `recordWarningFacts` |
| `composite_capture_recorder.go:115` | counter + first-occurrence slog wiring | `newCompositeCaptureRecorder` |
| `too_large_warning.go:16,34` | missing/over-ceiling warning generations | `stateTooLargeWarningGeneration`, `stateMissingWarningGeneration` |
| `doc.go` | package contract | — |

## 2. Fact kinds and versions

All 8 `terraform_state` kinds are `1.0.0`
(`TerraformState*SchemaVersion` in `facts/cloud/terraform_state.go`):

| Kind | Constructor | Producer record |
| --- | --- | --- |
| `terraform_state_snapshot` | `parser.go:234` (`snapshotFact`) | `terraform_state.go:116` |
| `terraform_state_resource` | `resources.go:220` | `terraform_state.go:116` |
| `terraform_state_output` | `outputs.go:130` | `terraform_state.go:116` |
| `terraform_state_module` | `modules.go:33` | `terraform_state.go:116` |
| `terraform_state_provider_binding` | `providers.go:67` | `terraform_state.go:116` |
| `terraform_state_tag_observation` | `tags.go:152` | `terraform_state.go:116` |
| `terraform_state_warning` | `warnings.go:99`, `warning_fact.go:31` | `terraform_state.go:116` |
| `terraform_state_candidate` | `go/internal/collector/repo/git/tfstate_candidate.go` (git collector) | `terraform_state.go:117` (`git-core`, `core_owned`) |

The family declaration is already cutover-shaped
(`AuthorityMode: core_issued_grant`,
`PlannedRepository: eshu-hq/eshu-collector-terraform-state`):
`terraform_state.go:116` binds the 7 owned kinds (all but the candidate);
`terraform_state.go:117` binds the candidate to `git-core`. Owned kinds
excluding the candidate: **7**. Proposed grant identity (see
[PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md)): producer
`dev.eshu.collector.terraform-state`, scope `terraform_state`.

Shared-kind note: the same parser still emits 3 `incident_routing`
kinds at `1.0.0` from allowlisted applied resources
(`pagerduty_applied.go`; emission code untouched by the #9195 partial
reversal): `incident_routing.applied_pagerduty_resource` (`:159`),
`incident_routing.applied_alert_route` (`:231`),
`incident_routing.coverage_warning` (`:240`, unsupported `pagerduty_*`
types). The producer record binds them to this collector
(`routing.go:88` in `facts/incident`, same
`core_issued_grant`/`eshu-hq/eshu-collector-terraform-state`). The staged
grant draft covers the 7 `terraform_state_*` kinds **plus** 2 of the 3
`incident_routing.*` kinds at `1.0.0` (arbiter partial reversal on
#9195: `coverage_warning` dropped — pagerduty owns it (a live
installer conflict in a QA environment proved dual emission);
`applied_*` kept, no other owner; §8.2 records the ruling).

Consumption note: the projector consumes 6 kinds (snapshot, resource,
module, output, tag_observation, provider_binding — see §3).
`terraform_state_candidate` and `terraform_state_warning` are
intentionally unconsumed (family `Disposition: unconsumed`).

## 3. Consumers (must agree at cutover)

- Canonical projection: `go/internal/projector/canonical/terraform_state*.go`
  (`ExtractTerraformStateRows` in `terraform_state.go:37`; row shapes in
  `terraform_state_types.go`; ownership enrichment in
  `terraform_state_ownership.go`). Candidate and warning are typed-but-deferred
  with no read site.
- Decode seam: `go/internal/projector/decode/terraform_state.go` (6
  wrappers for the consumed kinds; no wrapper for candidate/warning by
  design, `:17-25`).
- Query/read: `GET /api/v0/iac/resources`
  (`go/internal/query/iac/resources.go`, handler in `handler.go:54`).
- Reducer domain: `config_state_drift`
  (`domain.go:56` in `reducer/contract`; handler
  `reducer/tfconfigstate/terraform_config_state_drift.go`).
  Drift ownership resolves through the `ScopeLocatorHash` join
  (`identity.go:137`); a formula change silently rejects every intent
  (`ErrNoConfigRepoOwnsBackend`, identity.go doc).
- Projection hook (contract metadata): `terraform_relationship_projection`
  (`terraform_state.go:100`).
- Incident-routing consumers for the §2 shared kinds: reducer
  `go/internal/reducer/incident/` (`incident_routing_materialization`
  domain) decodes applied evidence; read surface
  `GET /api/v0/incidents/{incident_id}/context`.
- Coordinator: `go/internal/coordinator/tfstate_service.go`
  (`scheduleTerraformStateWork`) plus `coordinator/planner/tfstate/`
  plans claim rows per active instance; stays core-owned.
- Entrypoint contract: **no `terraform-state` entry exists** in
  `go/internal/collector/entrypoints/collector_entrypoints.yaml` today
  (only `pagerduty` and `jira` entries) — open gap, §8.5.

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (via `service.go:84` claimed
  wiring).
- Claim lifecycle: `collector.ClaimedService` (`service.go:115-130`;
  `postgres.NewWorkflowControlStore`, heartbeat strictly below lease TTL
  per cmd `AGENTS.md`).
- Prior snapshots, backend facts, git readiness:
  `postgres.TerraformStatePriorSnapshotReader`,
  `TerraformStateBackendFactReader`, `TerraformStateGitReadinessChecker`
  (`service.go:93-99`) — core-owned reads, never producer writes.
- In-tree transient state only: temp-gob spools
  (`fact_spool.go`, `terraformstate/fact_stream.go`; deleted on close)
  and per-call `stateParser` accumulators; both reset per claim and never
  persist. `AllowEmptySuccessor` (`source.go:119-133`,
  `source_helpers.go:44-65`) is an explicit operator attestation stamped
  onto the generation for the legitimate-empty shape only, not stored
  state.

Admission, persistence, reducer queue, projection, and retraction remain
Eshu core per the extraction policy. The external producer must submit a
bounded result; it never receives Postgres, graph, queue, lease, reducer,
API, MCP, or workflow handles.

## 5. Protocols

- S3: one exact-object `GetObject` per claim
  (`aws_s3.go:38`, `source_s3.go:133`), conditional `IfNoneMatch` from
  prior ETag (`source.go:239` → `Unchanged` generation, no fact writes);
  prefix-only keys and workspace layouts rejected at discovery.
- DynamoDB: read-only `GetItem` on the lock table (`aws_dynamodb.go:46`),
  `Digest` observational evidence only — freshness decisions use the
  opened body plus durable generation data.
- Local: exact operator-approved absolute files, symlink refused
  (`source_local.go:88-99`); repo-local `.tfstate` only via approved
  Git-local candidates (`local_candidates.go`), else `state_in_vcs`
  warning evidence, never a silent read.
- Redaction bundle is startup-fatal: `ESHU_TFSTATE_REDACTION_KEY`,
  versioned `ESHU_TFSTATE_REDACTION_RULESET_VERSION`, and the packaged
  provider-schema bundle (`config.go:254-363`). Without the resolver
  every scalar redacts and state rows orphan from cloud resources.
- Target wire shape: `ESHU_COLLECTOR_INSTANCES_JSON` instance
  `Configuration.target_scopes[]` (`config.go:71-88`); per-claim target
  credentials resolve through `targetScopeSourceFactory` with unknown or
  ambiguous `target_scope_id` failing closed.
- Public target contract: `collector-sdk/v1alpha1`.
- Cassette replay: binary flag `-mode=cassette -cassette-file=<path>`
  (`main.go:72-88`, `service.go:33`).
- Claim IDs carry the `tfstate-claim-` namespace (`service.go:133`);
  heartbeat >= lease fails closed at startup (`config.go:148-154`).

## 6. Artifacts

- Today: single in-tree binary `collector-terraform-state`
  (`serviceName` in `main.go:26`). No published artifact from this PREP.
- In-tree `go/go.mod` already requires the supported SDK path at the
  exact cutover pins (`github.com/eshu-hq/eshu-sdk/collector
  v0.1.0-rc.3`, `factschema v0.1.0-rc.5`) — no `sdk/go` workspace
  replaces. The staged grant draft §"Proposed grant identity" states
  the same pins.
- At cutover: digest-pinned OCI artifact executed via the
  component-extension host `process` runner with the seeded binary and
  manifest. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` moving toward `ready_for_cutover`: SHIM_MAP plus producer-grant
and rollback drafts exist in-tree, and the in-tree parity pack landed
(`source_parity_8868_test.go`, `source_parity_8868_hostile_test.go`,
`testdata/8868-parity-tfstate.json` in
`go/internal/collector/terraformstate/`); remaining gates are the §8
prerequisites, the external `#4047` comparison on that pack, the
activation template (`#6710`, `#8894`), producer delegation (`#6709`),
and the per-collector cutover gate (`#4047`). Roadmap owners: `#6707`
inventory, `#6708` SDK/fact-schema, `#6709` producer delegation,
`#6710` template and compatibility harness, `#4047` per-collector
cutover gate.

## 8. Prerequisites for cutover

1. First-party producer delegation live (`#6709`); grant draft recorded
   in [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md) for the 7 owned
   `terraform_state_*` kinds plus the 2 kept co-emitted
   `incident_routing` kinds (`applied_*`; see item 2).
2. Grant-or-drop for the 3 co-emitted `incident_routing.*` kinds —
   **partially reversed** (arbiter verdict on #9195):
   `coverage_warning` dropped (pagerduty owns it — a live installer
   conflict in a QA environment proved dual emission), `applied_pagerduty_resource`
   and `applied_alert_route` kept (no other owner); the draft and the
   rollback revocation loop now cover those 2 at `1.0.0`.
3. Parity proof on representative + hostile inputs via a fixture pack and
   `source_parity` test (AC3, in-tree side) — **in-tree side landed**:
   `go/internal/collector/terraformstate/source_parity_8868_test.go`,
   `source_parity_8868_hostile_test.go`, and
   `testdata/8868-parity-tfstate.json` (focused proof:
   `go test ./internal/collector/terraformstate/
   ./internal/collector/tfstateruntime/ -count=1` PASS). The `#4047`
   external comparison on that pack stays open.
4. Activation entry from the `#6710`/`#8894` template with the
   compatibility harness matrix (newest collector × oldest core and
   reverse), including redaction-bundle + schema-bundle provisioning
   (grant-draft checklist) and the `allow_empty_successor` attestation
   carry-over.
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Entrypoint contract: add the `terraform-state` entry to
   `collector_entrypoints.yaml` (or record why this collector is exempt)
   so the cutover comparison covers the same surface as `#8863`.
