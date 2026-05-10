# ADR: Terraform State Collector

**Date:** 2026-04-20
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md` — fact-field
  back-propagation source for the reducer/consumer contract this ADR now
  incorporates directly.

---

## Status Review (2026-05-10)

**Current disposition:** Design accepted; runtime partially implemented; discovery
and deployment model amended.

The Terraform-state runtime now exists in this repo. The implementation has
landed exact local and S3 source readers, streaming parse/redaction primitives,
Terraform-state fact identity, graph-backed S3 backend discovery from Git
evidence, claimed runtime wiring, S3 conditional reads, and DynamoDB lock
metadata reads.

This review amends the original discovery posture. Eshu should use the Git
collector's repository index as a discovery surface for repo-local
`terraform.tfstate` candidates when such files exist, but it must not treat
those files as normal Git content and must not persist raw state bytes. Local
state candidates are advisory until approved by operator policy or collector
configuration. Approved candidates are then opened by the Terraform-state
collector and pass through the same streaming redaction path as explicit local
sources.

The deployment model is also amended. Terraform-state and future cloud
collectors must support both:

1. a central collector deployment that assumes account-scoped read roles, and
2. account-local or provider-local collector deployments for organizations that
   prefer narrower blast radius or cannot grant broad cross-account trust.

AWS remains the first concrete backend, but target scope, credential ownership,
and candidate approval must be provider-neutral so GCS, Azure Blob, Terraform
Cloud, and later cloud scanners can reuse the same control-plane shape.

## Security Review (2026-05-10)

**Scope:** Closes the `needs-security-review` gate for issue #46 (tfstate
reader stack). Covers the merged reader/parser primitives (PRs #84, #147,
#148, #150), the redaction-before-emission path, the local and S3 source
seams, the DynamoDB lock-metadata reader, and the no-plaintext persistence
regression. New tfstate code merged after this review must either fall under
the same mitigations or be added to this section.

### Threat Model

The Terraform state collector reads source-of-truth secrets material (real
ARNs, resource identifiers, sensitive output values, occasionally raw
credentials authors stored as Terraform outputs) and must never let any of
that material reach the Eshu persistence boundary in plaintext. The collector
also holds cloud read credentials and therefore inherits the cloud-collector
threat surface (compromise, blast radius, write attempts).

The mitigations below name the file (and where useful, the symbol) that
enforces each one. Cites are intentionally symbol-level rather than line-level
so they survive minor refactors.

### Risks

- **Plaintext persistence of secrets.** Mitigated by parser-side redaction
  applied **before** any Eshu persistence boundary. The streaming parser
  routes every scalar through the `go/internal/redact` policy
  (`go/internal/collector/terraformstate/{parser.go,outputs.go,resources.go,
  tags.go,attributes.go}`); operator-policy redaction is applied even to
  outputs whose Terraform `sensitive` flag is `false` — the gap PR #150 closed
  via `outputs.go` (`Classify` call against `redact.SchemaKnown`). The
  end-to-end regression in
  `go/internal/storage/postgres/tfstate_no_plaintext_persistence_test.go`
  (function `TestTerraformStateClaimedCommitDoesNotPersistPlaintextSecrets`,
  helper `assertExecArgsDoNotContain`) drives a real claim through the
  `tfstateruntime.ClaimedSource` and inspects every SQL argument bound to
  `fact_records` and related persistence calls for the plaintext needles
  the fixture state contained.
- **Raw state bytes leaking via the content store.** Mitigated by the
  collector wiring in `go/cmd/collector-terraform-state/service.go` —
  `buildClaimedService` does not pass a content-store writer into the
  tfstate fact path. Only redacted facts and warning records cross the
  persistence boundary. The parser is streaming
  (`terraformstate.ParseStream` + `FactSink` in `parser.go`), so even the
  in-process working set does not accumulate the full payload.
- **Locator / path disclosure.** Mitigated by locator hashing. Full S3
  URLs, bucket names, keys, version IDs, and absolute local paths are
  hashed before they enter fact payloads or source refs — see
  `LocatorHash` in `go/internal/collector/terraformstate/identity.go` and
  its callers in `parser.go` and `candidate_identity.go`. Warning facts
  share the same locator-hash discipline through `warning_fact.go`.
- **Unbounded reads / OOM via giant state.** Mitigated by a read-time size
  ceiling enforced inside the stream, not just at metadata precheck.
  `ErrStateTooLarge` is declared in `source_local.go` and returned from both
  local and S3 readers (`source_local.go:71`, `source_s3.go:153`); the
  `sizeEnforcingReadCloser` in `source_limit.go` enforces the ceiling at
  read time. The runtime translates
  the error into a `terraform_state_warning` fact with
  `warning_kind=state_too_large` and completes the claim cleanly — no
  retry storm. The default ceiling is 512 MB and is operator-tunable via
  `ESHU_TFSTATE_SOURCE_MAX_BYTES`.
- **Broad S3 / DynamoDB blast radius.** Mitigated by exact-locator reads
  with no prefix scanning. `source_s3.go` validates bucket and key
  literally, propagates `IfNoneMatch`, and rejects any write-shaped client
  configuration. The S3 adapter at `go/cmd/collector-terraform-state/aws_s3.go`
  exposes only `GetObject`. The DynamoDB adapter at
  `go/cmd/collector-terraform-state/aws_dynamodb.go` is read-only `GetItem`.
  Local source reads (`source_local.go`) reject any path that is not an
  operator-approved regular file (`validateRegularLocalStatePath`); symlinks
  and non-regular files are refused at open time.
- **Local-state ambient discovery.** Mitigated by explicit operator
  approval. The Git collector flags repo-local `*.tfstate` files as
  advisory `terraform_state_candidate` metadata, but the runtime opens one
  only when `discovery.local_state_candidates.mode` is `approved_candidates`
  and the exact `repo_id` plus repo-relative `path` are listed. Every
  approved local read also emits a `terraform_state_warning` with
  `warning_kind=state_in_vcs` so operators can drive those repos off
  in-tree state. `.eshuignore` excludes `*.tfstate` from normal Git
  collection by default.
- **Provider-schema drift.** Mitigated by fail-closed conservative
  redaction. Unknown scalar attributes are redacted with reason
  `unknown_provider_schema` (constant `ReasonUnknownProviderSchema` in
  `go/internal/redact/policy.go`); unknown composite attributes are
  dropped to warning facts rather than persisted as raw values. The
  `eshu_dp_tfstate_redactions_applied_total{reason}` counter and the
  `EshuTfstateUnknownProviderSchemaSurge` alert in
  `deploy/observability/alerts.yaml` let operators see when a new provider
  shape has landed.
- **Stale / poisoned conditional reads.** Mitigated by ETag-driven
  conditional GETs. `source_s3.go` carries the prior ETag in
  `IfNoneMatch`; an S3 not-modified response increments
  `eshu_dp_tfstate_s3_conditional_get_not_modified_total` and skips the
  read. A mismatched ETag forces a full re-read and a new redacted parse.
- **Credential compromise / cross-account abuse.** Mitigated by target-scope
  routing in
  `go/cmd/collector-terraform-state/target_scope_source_factory.go`.
  Candidates are routed to the configured scope by explicit
  `target_scope_id` or by `allowed_backends` + `allowed_regions` match;
  ambiguous matches fail before the object is opened. Central scopes
  require `central_assume_role` with an optional `external_id`;
  account-local scopes use workload identity in that account. The runtime
  refuses to mix the legacy `aws.role_arn` field with `target_scopes`.

### Out of Scope

- The 100 MB peak-memory fixture benchmark mentioned in the original issue
  is no longer required for closure: PR #148 moved the runtime to the
  token-level streaming path (`ParseStream` + `FactSink`), so working-set
  memory is bounded by the largest scalar plus the emitted fact batch, not
  the full state payload. If a formal benchmark gate is wanted later, file
  a follow-up issue.
- Broad in-Git `.tfstate` parsing is intentionally **not** in scope here;
  candidate discovery is tracked separately in #140 and gated by the
  `approved_candidates` policy above.

### Sign-off

| Role | Reviewer | Date |
|------|----------|------|
| Platform Engineering | Allen Sanabria | 2026-05-10 |
| Security | Allen Sanabria | 2026-05-10 |

Sign-off removes the `needs-security-review` label on issue #46.

## Status Review (2026-05-03)

**Current disposition:** Design accepted; runtime not implemented.

The shared primitives exist, including `state_snapshot` scope, Terraform-state
collector identity, and the reducer phase contract. The actual collector runtime
does not exist in this repo yet: there is no `go/cmd/collector-terraform-state`
or `go/internal/collector/terraformstate` implementation.

**Remaining work:** implement `StateSource`, local and S3 readers, streaming
parser/redaction, fact emission, coordinator claim integration, Terragrunt,
output/module/tag coverage, DSL consumers, tests, telemetry, and operator docs.

## Context

Eshu already ingests Terraform **configuration** via the Git
collector and the Terraform config parser. That surfaces *intent*: what
resources the author declared, which modules they used, which backends were
wired, which `app_name` values were assigned.

Terraform **state** is a different source of truth. It reports what
Terraform believes it actually provisioned: real ARNs, resource IDs,
endpoints, names, tags, and the serial number of the last apply. Without
state, the platform can describe what the code *said*; it cannot describe what
the code *produced*.

The multi-source correlation ADR already named `terraform_state` as a
first-class collector family with its own `CollectorKind` and a
`state_snapshot` scope. The workflow coordinator ADR already defined the
runtime shape for a `collector-terraform-state` instance, its claim contract,
and how it plugs into the shared reducer and read paths.

This ADR is the collector-specific design ADR those two ADRs deferred. It
defines how the Terraform state collector observes source truth, how it is
configured, how it reads state across heterogeneous backends, how it protects
secrets, what facts it emits, and how it cooperates with the workflow
coordinator and the correlation DSL.

### What Is Already Decided

The following are contracts inherited from prior ADRs. This ADR does not
revisit them:

1. Collectors observe source truth and emit typed facts. They do not write
   canonical graph rows directly.
2. The reducer owns cross-source correlation. Drift detection, state-versus-
   config comparison, and state-versus-cloud comparison live in the
   correlation DSL, not in the state collector.
3. The workflow coordinator owns run creation, claim issuance, fencing,
   completeness, and operator-visible status. The state collector claims
   bounded work from the coordinator; it does not schedule itself.
4. `ScopeKind.state_snapshot` and `CollectorKind.terraform_state` already
   exist in the shared identity enums.
5. Collector instances are declarative: desired state lives in configuration,
   observed state lives in Postgres control-plane rows.

### What This ADR Decides

1. The set of state backends supported at launch.
2. How the collector discovers which state objects to read.
3. The `StateSource` abstraction that unifies local, S3/DynamoDB, and
   Terragrunt-resolved backends under one reader contract.
4. Scope and generation identity for a state snapshot.
5. Fact shapes emitted from state.
6. Secret redaction policy, because state files routinely contain plaintext
   credentials.
7. Streaming and size-discipline rules for large state files.
8. The correlation anchors state contributes to the DSL.
9. The phased rollout plan.
10. The difference between state discovery, state candidate approval, and state
    ingestion.
11. The credential and deployment model for central and account-local
    collectors.
12. The provider-neutral target-scope contract shared with future cloud
    scanners.

---

## Problem Statement

Without a Terraform state collector, Eshu cannot truthfully
answer:

- Which real ARN corresponds to the resource declared in module X?
- Which concrete `aws_lb` hostname serves the release declared by chart Y?
- Which RDS endpoint backs the `api-node-chat` service in production?
- Did the last Terraform apply succeed, and what serial does the graph
  reflect?
- Does the code intent match what Terraform believes is deployed?
- Does Terraform believe it owns resources the AWS scanner has observed?

Those questions are the foundation for every cross-source correlation the
platform wants to support. They are also the foundation for drift detection,
orphan detection, and unmanaged-resource detection once the AWS scanner is
online.

The platform must now commit to:

- how the collector reads state without becoming a mutation risk
- how it handles the real-world heterogeneity of where state actually lives
- how it refuses to leak plaintext secrets present in state payloads
- how it stays within the facts-first, collector-local contract already
  established by the Git collector

---

## Decision

### Support Three Backend Families At Launch

The state collector should support, at launch:

1. **Local state files** (`terraform.tfstate` committed or generated inside a
   repository workspace)
2. **Amazon S3 remote backend** (with optional DynamoDB lock table used for
   read-only metadata only)
3. **Terragrunt-wrapped backends** that resolve to local or S3

The following are explicit non-goals for launch (deferred to later phases):

- Terraform Cloud / Terraform Enterprise workspace API
- Google Cloud Storage backend
- Azure Blob Storage backend
- HTTP backend
- Consul backend
- Postgres backend

Terragrunt is not a separate backend. Its `remote_state` block resolves to a
concrete backend (typically S3). The collector consumes the *resolved* backend
configuration. Terragrunt discovery belongs to the Terraform config parser,
which already exists in the Git collector surface.

### Unified `StateSource` Reader Abstraction

The collector should expose one reader interface that every backend
implementation satisfies:

```go
// go/internal/collector/terraformstate/source.go
type StateSource interface {
    // Identity returns the durable key for this state snapshot.
    Identity() StateKey

    // Open returns a streaming reader over the raw state payload and the
    // metadata required for generation assignment and freshness tracking.
    Open(ctx context.Context) (io.ReadCloser, StateMetadata, error)
}

type StateKey struct {
    BackendKind BackendKind // "local" | "s3" | "terragrunt"
    Locator     string      // canonical locator (e.g. s3://bucket/key or repo-relative path)
    Serial      int64       // terraform state serial; monotonic per locator
    Lineage     string      // terraform state lineage UUID; identifies the chain
}

type StateMetadata struct {
    ObservedAt     time.Time
    Size           int64
    ETag           string        // for S3; empty otherwise
    LastModified   time.Time
    LockDigest     string        // optional; from DynamoDB lock table, read-only
    BackendConfig  BackendConfig // full config that produced this source
}
```

Implementations at launch:

- `localStateSource` — reads an exact operator-approved local file. The Git
  collector may discover candidate paths, but raw `.tfstate` must not flow
  through normal Git content persistence. Approved local candidates are handed
  to this source reader and parsed through the Terraform-state redaction path.
- `s3StateSource` — uses AWS SDK v2 `s3.GetObject` with `If-None-Match`
  against the previously observed ETag. Optional DynamoDB `GetItem` against
  the lock table reads `LockID` and `Digest` for metadata only. The
  collector must never call `PutItem`, `UpdateItem`, `DeleteItem`,
  `LockTable` APIs, or issue any state write on S3.
- `terragruntResolvedSource` — thin shim that resolves a Terragrunt remote
  state block (already parsed by the config collector) into the underlying
  `localStateSource` or `s3StateSource`. Terragrunt does not get its own
  reader; it gets a resolver.

### Discovery: Find Candidates First, Ingest Approved Sources

The collector must answer "which state objects should I read?" before any
backend traffic. The answer has two stages:

1. **Discovery** records possible state locations and why Eshu believes they
   matter.
2. **Ingestion** opens an approved exact source and emits redacted facts.

This distinction matters because Terraform state is high-risk source material.
Eshu should discover state aggressively enough to be useful as the central
evidence map, but raw state must only cross the Terraform-state collector's
reader and parser boundary.

Discovery is layered:

1. **Explicit sources** in collector instance configuration win when present.
   Operators may pin a state source when the graph is cold, when a state lives
   outside any scanned repository, or when testing.
2. **Git-observed backend facts** are the normal path for remote state. The
   collector queries Postgres for Terraform `backend` and Terragrunt
   `remote_state` facts emitted by the Git collector, producing exact backend
   candidates such as an S3 bucket/key/region tuple.
3. **Git-indexed repo-local state candidates** are advisory. If the Git
   collector has already indexed a repository file set and sees
   `terraform.tfstate` or `*.tfstate`, Eshu may record a candidate row with
   repo, path hash, observed generation, and warning metadata. It must not
   persist the raw state as normal file content, parse it in the Git collector,
   or emit Terraform-state facts until the candidate is approved by policy or
   collector configuration.
4. **Declarative seeds** remain a bounded bootstrap fallback for the first run
   where no Git generation has completed yet.

The collector must never crawl S3 buckets, list unknown prefixes, or probe
unknown accounts. Every state object read must trace back to an explicit
operator source, a Git-observed backend fact, or an approved repo-local
candidate created by a prior Git discovery run.

Candidate records should carry enough information for operators and MCP tools
to make a decision without exposing state contents:

- candidate ID
- source type (`explicit`, `git_backend`, `git_local_file`,
  `terragrunt_remote_state`)
- backend kind (`local`, `s3`, later `gcs`, `azurerm`, `terraform_cloud`)
- repo or target scope
- locator hash and safe display label
- discovery generation
- approval status (`discovered`, `approved`, `ignored`, `ingested`,
  `failed`)
- warning flags such as `state_in_vcs`, `unsupported_backend`,
  `dynamic_backend`, or `workspace_expansion_required`

Default ingestion mode should be conservative:

- `discover_only`: record candidates and warnings; open nothing.
- `explicit_sources`: open only configured exact sources.
- `approved_candidates`: open exact candidates approved by policy or operator
  action.

A future `auto_ingest_git_candidates` mode may exist for tightly controlled
environments, but it must be off by default and guarded by redaction policy,
size ceiling, path allowlists, and security review.

### Coordinator Run Dependencies

Because graph-backed discovery depends on Git collector output, the state
collector run must declare that dependency. The workflow coordinator already
supports run dependencies. The state collector must:

- refuse to run for a scope until the upstream Git generation for that scope
  has produced `canonical_nodes_committed`
- surface a readable "waiting on git generation X" status when blocked
- not invent fallback behavior that bypasses this gate

This prevents the platform from emitting state facts against an empty graph
and landing correlation decisions with missing upstream evidence.

### Scope And Generation Identity

Scope:

- `ScopeKind.state_snapshot`
- scope identity = `(backend_kind, locator)`
  - `(local, repo_id, workspace_path)`
  - `(s3, bucket, key, region)`
  - Terragrunt resolves to one of the above; does not produce its own scope

Generation:

- `generation = state.serial`
- state serial is monotonic by Terraform's own contract; the collector never
  invents a generation
- `lineage_uuid` is carried alongside and stored on every emitted fact so
  lineage rotations are detectable and not silently merged
- serial rollbacks (observed serial less than previously indexed serial for
  the same lineage) are rejected and emitted as a `failure_class` of
  `state_serial_regression`

### Fact Shapes

The collector emits a bounded set of typed facts. Names are scoped to the
collector family.

| Fact Kind | Purpose |
| --- | --- |
| `terraform_state_snapshot` | one per observed state object; carries serial, lineage, backend metadata, size, observed timestamp |
| `terraform_state_resource` | one per resource instance in the state (`type`, `name`, `module`, `provider`, `mode`, attributes; includes `arn`, `id`, `name`, `tags` when present) |
| `terraform_state_output` | one per named output (name, sensitive flag, value digest) |
| `terraform_state_module` | one per module block (source, version, path, inputs digest) |
| `terraform_state_provider_binding` | one per provider configuration referenced by state (alias, region, assume role ARN when present, account hint) |
| `terraform_state_tag_observation` | tag key/value pairs extracted from resource attributes; separate fact for correlation indexing |
| `terraform_state_warning` | anti-pattern signals such as `state_in_vcs`, `plaintext_sensitive_value`, `lineage_rotation`, `serial_regression` |

Each fact carries:

- the shared fact envelope already used by the Git collector
- `scope_id`, `generation_id`, `collector_kind = terraform_state`
- `lineage_uuid`, `serial`, `backend_kind`, `locator`
- observed timestamp of the state snapshot

Facts are emitted in streaming fashion during state parsing; the collector
must not buffer an entire state file in memory as Go structs before emitting.

### Required Reducer/Consumer Contract Fields

The accepted multi-source reducer/consumer ADR freezes the tfstate envelope
fields that downstream joins depend on. These are not optional derived
attributes; they are first-class collector outputs.

Add to `terraform_state_resource`:

- `provider_resolved_arn` — nullable string; populated when provider schema
  can deterministically resolve the resource identity into an ARN
- `module_source_path` — nullable string; normalized terminal module source
  path such as `github.com/org/repo//modules/service?ref=v1.2.3`
- `module_source_kind` — enum `{git, registry, local, unknown}`
- `correlation_anchors` — non-empty `[]string` when anchors are known, for
  example `[arn:..., tag:Service=foo, tag:Environment=prod]`

Add to `terraform_state_module`:

- `source_kind`
- `source_path`

Shared envelope expectations for every tfstate fact:

- `scope_id`
- `collector_kind=terraform_state`
- `generation_id`
- `fence_token`
- `source_confidence`

Reducer/consumer implication:

- `provider_resolved_arn` is the deterministic join key for
  `trace_arn_to_code`, `find_orphaned_state`, and `find_unmanaged_resources`
- `module_source_path` and `module_source_kind` are the deterministic join
  keys from Terraform state back to Git-owned module sources
- `correlation_anchors` are the bounded DSL inputs for
  `cross_source_anchor_ready`; they must not be reconstructed later at query
  time from opaque attribute maps

### Secret Redaction Policy

Terraform state routinely contains plaintext secrets. The collector must
refuse to persist those values, even transiently.

Mandatory redaction rules:

1. **`sensitive = true` outputs:** value is replaced with `sha256:<hex>` of
   the raw bytes. The hash lets the reducer correlate outputs across
   generations without storing the value.
2. **Known-sensitive attribute keys** on any resource (`password`,
   `master_password`, `secret_key`, `private_key`, `token`, `auth_token`,
   `access_key`, `*_secret`, `*_password`, `credentials`): replaced with
   `sha256:<hex>`. The key list is versioned and extensible per provider.
3. **Unknown provider schemas:** when the collector cannot classify an
   attribute's sensitivity (for example, a third-party provider absent from
   the packaged schema set), the attribute map is emitted with non-scalar
   values dropped and scalar values hash-redacted. This is a conservative
   default; loss of non-sensitive attributes is preferable to leaking
   sensitive ones. A `terraform_state_warning` with `failure_class =
   unknown_provider_schema` accompanies the fact so operators can request
   schema coverage.
4. **Raw state payload content** is not persisted to the Eshu content store.
   Only redacted facts are persisted. This is a deviation from the Git
   collector's content-store-first rule and is intentional: the source
   material is too hazardous to hold.
5. **Logs and spans must never carry redacted values, raw attribute values,
   or output values.** Telemetry carries hashes, sizes, and counts only.

Redaction policy must be configurable without weakening the defaults. The
collector ships with a baseline sensitive-key list and provider-schema rules,
then lets operators add field names, path patterns, provider attributes, and
resource-type rules that should always redact or drop. User-supplied rules are
additive unless an explicit security review approves a narrower override. If a
rule conflict cannot be resolved safely, the collector redacts or drops the
value and emits a warning fact.

### Streaming And Size Discipline

State files can exceed 100 MB for monolith workspaces. Treat size as a
first-class concern.

Required behavior:

- Parse state using `json.Decoder` token streaming. No `json.Unmarshal` into
  a single struct.
- Emit facts on resource boundaries so the collector's memory footprint is
  bounded by the largest single resource attribute map, not the entire state.
- A hard size ceiling per state file defaults to **512 MB** and is
  configurable per collector instance. States exceeding the ceiling are
  rejected with `failure_class = state_too_large` and a warning fact.
- Observed size is recorded on `terraform_state_snapshot` and as a histogram
  metric.

### Correlation Anchors Surfaced To The DSL

The collector emits raw correlation keys. The correlation DSL normalizes.

Anchors this collector contributes:

- `arn` (any attribute named `arn`, `role_arn`, `target_group_arn`, etc.)
- `aws_account_id` (parsed from any observed ARN)
- `region` (from provider binding and from ARN segments)
- `resource_id` (Terraform `id` attribute)
- `resource_name` (Terraform `name` attribute)
- `tags` (key/value pairs; emitted unchanged, normalized by DSL)
- `module_source_path` (module provenance)
- `module_version`
- `provider_alias`
- `workspace` (Terraform workspace name, when not `default`)
- `lineage_uuid` (join across serial boundaries)
- `output_name` (for DSL rules that match by output identity)

Tag normalization, value aliasing, and precedence rules are **not** this
collector's concern. Those belong to the correlation DSL (see follow-up tag
taxonomy addendum to the DSL ADR).

### Drift Is Not Collected Here

This collector emits observed state. It does not compute drift.

- Drift between state and Terraform config lives in the reducer as a DSL
  rule joining `terraform_config_resource` facts with
  `terraform_state_resource` facts on `(module_source_path, resource_type,
  resource_name)`.
- Drift between state and cloud observation lives in the reducer as a DSL
  rule joining `terraform_state_resource` with AWS scanner facts on `arn`.
- Orphan detection (state resource with no config backing) and unmanaged
  detection (cloud resource with no state backing) also live in the DSL.

The collector's only job is to make those joins possible with clean, typed,
redacted, provenance-preserving facts.

---

## Architecture

### Runtime Shape

The collector is a long-running service:

- binary: `/usr/local/bin/eshu-collector-terraform-state`
- package: `go/cmd/collector-terraform-state/`
- internal: `go/internal/collector/terraformstate/`
- Kubernetes: `Deployment` (not `StatefulSet` — no workspace PVC needed;
  state is read from exact approved sources such as local files or S3 objects)
- replicas: `>= 1`, horizontally scalable under the coordinator claim model

### Package Layout

```text
go/
  cmd/
    collector-terraform-state/
      main.go
  internal/
    collector/
      terraformstate/
        source.go            # StateSource interface
        source_local.go      # exact approved local reader
        source_s3.go         # S3 + DynamoDB (read-only) reader
        source_terragrunt.go # resolver shim
        discovery.go         # graph-backed + override-backed discovery
        parser.go            # streaming json.Decoder state parser
        redact.go            # secret redaction policy
        facts.go             # fact envelope builders
        service.go           # coordinator claim loop
        telemetry.go         # span/metric helpers
```

### Collector Instance Configuration

```yaml
collectors:
  - id: tfstate-prod
    kind: terraform_state
    mode: scheduled
    enabled: true
    bootstrap: true
    config:
      # Discovery
      discovery:
        graph: true
        seeds:
          # Explicit overrides; removed once graph is warm
          - kind: s3
            bucket: app-tfstate-prod
            key: services/api-node-chat/terraform.tfstate
            region: us-east-1
            dynamodb_lock_table: app-tfstate-locks
        local_repos:
          # Limit local discovery to these repo IDs
          - example/platform-infra

      # Credentials for S3 backends
      aws:
        role_arn: arn:aws:iam::123456789012:role/eshu-tfstate-reader
        external_id: ${secret:aws.tfstate.external_id}

      # Safety
      max_state_bytes: 536870912   # 512 MB
      refresh_interval: 15m
      schema_sources:
        - path: /etc/eshu/terraform-schemas
```

Multiple instances allowed when states live across accounts or backends with
incompatible credentials.

### Deployment And Credential Model

The Terraform-state collector must support two deployment patterns.

**Central collector with cross-account roles.** One deployment runs in the Eshu
control-plane account or cluster. Each target account exposes a read-only role
that the collector can assume for a bounded target scope. The collector assumes
the target role per claim or per short-lived credential cache window, records
the target account and role ARN as source metadata, and drops credentials when
the claim is released.

**Account-local or provider-local collectors.** A customer may deploy the
collector inside each account, project, subscription, or cluster. This reduces
cross-account trust and keeps the blast radius local. The same claim and fact
contracts still apply; only credential acquisition changes.

The control plane should model this as a provider-neutral target scope instead
of an AWS-only shape:

| Field | Purpose |
| --- | --- |
| `provider` | `aws`, later `gcp`, `azure`, `terraform_cloud` |
| `target_scope_id` | Stable Eshu ID for the account, project, subscription, org, or workspace |
| `credential_mode` | `central_assume_role`, `local_workload_identity`, `static_external` only if explicitly approved |
| `allowed_backends` | State backends this collector may open |
| `allowed_regions` / `locations` | Regions or locations in scope |
| `source_allowlist` | Exact bucket/key/path/workspace patterns allowed for ingestion |
| `redaction_policy_ref` | Versioned redaction policy used by this target |

AWS launch behavior:

- In Kubernetes on EKS, the pod should use either an IRSA service-account role
  annotation (`eks.amazonaws.com/role-arn`) or EKS Pod Identity. That role may
  read same-account state directly or call `sts:AssumeRole` into target
  accounts.
- Target roles must be read-only. For S3 state, grant `s3:GetObject` for the
  configured key and `s3:GetObjectVersion` when versioned reads are enabled.
  `s3:ListBucket` is optional and must be scoped to known prefixes; it is not a
  discovery mechanism.
- If the state object uses SSE-KMS, the target role needs `kms:Decrypt` for the
  relevant key. It does not need KMS write permissions.
- DynamoDB lock metadata is read-only. Grant `dynamodb:GetItem` and
  `dynamodb:DescribeTable` on configured tables when lock metadata is enabled.
  Do not grant `PutItem`, `UpdateItem`, or `DeleteItem`.
- Cross-account trusts should require an external ID or an equivalent customer
  tenant guard where applicable.

This model intentionally mirrors the AWS cloud scanner ADR. Terraform-state
collection and cloud scanning both need scoped target inventory, short-lived
credentials, auditable role assumptions, and least-privilege read policies.

### Control Flow

1. Coordinator issues a claim for a `(tfstate instance, scope batch)` unit
   of work.
2. Discovery produces the candidate `StateKey` list for that batch (graph +
   overrides, deduplicated).
3. For each key:
   a. Open the `StateSource`, get a streaming reader and metadata.
   b. Compare observed `(lineage_uuid, serial)` against the prior indexed
      values. If equal, emit a `terraform_state_snapshot` fact with
      `unchanged = true` and release the claim segment. No further facts.
   c. If changed, stream-parse; emit facts per resource / output / module /
      provider binding / tag observation.
   d. Apply redaction in the parse loop.
   e. Enqueue emitted facts to the shared fact queue.
4. Coordinator receives completion acknowledgment with counts, serial, and
   lineage.
5. Reducer consumes state facts via the existing queue substrate. No change
   to reducer wiring beyond new domain handlers in the DSL.

### S3 Read-Only Posture

IAM policy for the state collector's assume-role target must grant only the
read actions required by the enabled source path:

- `s3:GetObject` on configured state objects
- `s3:GetObjectVersion` when the collector is allowed to read a specific S3
  version
- `s3:ListBucket` only when needed for a known workspace or prefix check, and
  always scoped to configured prefixes
- `dynamodb:DescribeTable`, `dynamodb:GetItem`, and optionally
  `dynamodb:Query` on configured lock tables
- `kms:Decrypt` when the state object is encrypted with a customer-managed KMS
  key

It must not grant:

- any `s3:PutObject`, `s3:DeleteObject`, or bucket-level write
- any `dynamodb:PutItem`, `dynamodb:UpdateItem`, `dynamodb:DeleteItem`
- any `kms:Encrypt`, `kms:GenerateDataKey`, or KMS administration action
- any `LockTable` or transaction APIs on the lock table

The collector code must also carry a runtime guard that rejects backend
configurations which claim write permissions. This is defense in depth; the
IAM policy is the primary control.

### Local State Caveat

Local state committed to a repository is a security anti-pattern. Eshu should
still surface it because the evidence is important, but discovery and ingestion
are separate:

- the Git collector may record a repo-local state candidate and warning
- the Terraform-state collector may open it only after explicit approval or
  policy approval
- any approved local state emits a `terraform_state_warning` with
  `warning_kind = state_in_vcs`
- the raw file is never stored as normal Git content

Local state discovered outside a scanned repository (for example, a path
mounted into the collector container) is rejected unless explicitly listed as
a seed.

---

## Invariants

After this collector lands, the following must hold:

1. The collector never issues a write to any state backend. No `PutObject`,
   no `PutItem`, no lock acquisition.
2. No raw state payload is persisted to the Eshu content store or to any
   ESHU-owned database column.
3. Every emitted fact carries `scope_id`, `generation_id` (equal to state
   serial), and `lineage_uuid`.
4. No sensitive attribute value or output value is emitted in plaintext.
   Redaction is enforced in the parser path, not as a post-hoc filter.
5. No state-level fact is emitted before the parser confirms a non-regressing
   `(lineage_uuid, serial)` pair.
6. Ingestion may not read a state object that was not produced by an explicit
   source, a Git-collector-observed backend fact, or an approved repo-local
   state candidate.
7. The collector does not compute drift. Drift lives in the reducer.
8. Collector runs are gated on upstream Git generation readiness for the
   corresponding scope when graph-backed discovery is in use.
9. Claim ownership, fencing, and completeness flow through the workflow
   coordinator's shared contract. The collector does not invent its own.

---

## Observability Requirements

Metrics (prefix `eshu_dp_tfstate_`):

- `snapshots_observed_total{backend_kind, result}`
- `snapshot_bytes_bucket` (histogram of raw state size)
- `resources_emitted_total{backend_kind}`
- `outputs_emitted_total{backend_kind}`
- `redactions_applied_total{reason}`
- `warnings_emitted_total{warning_kind}`
- `backend_errors_total{backend_kind, error_class}`
- `discovery_candidates_total{source}` (`graph` vs `seed`)
- `parse_duration_seconds_bucket{backend_kind}`
- `serial_regressions_total`
- `lineage_rotations_total`
- `unknown_provider_schema_total{provider}`
- `s3_conditional_get_not_modified_total` (successful 304 responses)

Spans:

- `tfstate.collector.claim.process`
- `tfstate.discovery.resolve`
- `tfstate.source.open`
- `tfstate.parser.stream`
- `tfstate.fact.emit_batch`

Structured logs must include `scope_id`, `generation_id` (serial),
`lineage_uuid`, `backend_kind`, `locator`. They must not include attribute
values, output values, or raw state bytes.

Admin status should expose, per instance:

- last observed serial per locator
- last observed timestamp per locator
- current claim ownership
- outstanding discovery candidates
- recent warnings summarized by `warning_kind`

---

## Explicit Non-Goals

1. Terraform Cloud / Terraform Enterprise workspace support at launch.
2. GCS, Azure Blob, HTTP, Consul, or Postgres backends at launch.
3. Writing or mutating any state backend.
4. Computing drift between state and config.
5. Computing drift between state and cloud truth.
6. Storing raw state payloads.
7. Replacing or bypassing the workflow coordinator's claim contract.
8. Defining tag normalization rules (deferred to correlation DSL).
9. Replacing the Git collector's role in emitting `terraform_config_*` facts.

---

## Rollout Plan

### Phase 1: Design And Identity

- publish this ADR
- extend `go/internal/scope` and `go/internal/facts` as needed to cover
  `state_snapshot` scope shape details
- add operator documentation for the collector instance config contract
- confirm the coordinator's run-dependency model covers `tfstate after git`
  scope gating
- add target-scope and credential documentation shared with the AWS scanner

### Phase 2: Local + S3 Reader, Streaming Parser, Redaction

- implement the `StateSource` interface with `localStateSource` and
  `s3StateSource`
- implement streaming parser + redaction
- emit `terraform_state_snapshot` and `terraform_state_resource` facts
- add additive user redaction policy support
- integrate with coordinator claim loop under dark-run mode
- unit + fixture tests for:
  - serial monotonicity
  - lineage rotation detection
  - redaction of known-sensitive keys
  - unknown-provider-schema conservative redaction
  - size-ceiling enforcement

### Phase 2b: Candidate Approval And Safe Discovery Expansion

- persist repo-local `.tfstate` discovery candidates without raw file content
- add candidate approval states and operator visibility
- wire approved repo-local candidates into exact local source reads
- keep `auto_ingest_git_candidates` disabled until security review approves it

### Phase 3: Terragrunt Resolution And Output/Module/Tag Facts

- add the Terragrunt resolver shim
- emit `terraform_state_output`, `terraform_state_module`,
  `terraform_state_provider_binding`, `terraform_state_tag_observation`,
  `terraform_state_warning`
- admin status surface complete

### Phase 4: Reducer / DSL Integration

- correlation DSL rule packs for state-versus-config drift
- correlation DSL rule packs for state-to-cloud joins by ARN (depends on AWS
  scanner ADR rollout)
- reducer domain wiring for `deployable_unit_correlation` to consume state
  facts

### Phase 5: Expanded Backends (Post-Launch)

- Terraform Cloud / Enterprise workspace API
- GCS, Azure Blob
- HTTP, Consul
- considered per demand; not committed here

---

## Consequences

### Positive

- Platform gains concrete resource identity (ARNs, IDs, endpoints) that the
  Git collector alone cannot provide.
- Correlation DSL gains the strongest join key available for cloud
  correlation (`arn`) without inferring it.
- Drift detection and orphan detection become expressible as DSL rules
  rather than custom code.
- The coordinator's run-dependency model is exercised end to end for the
  first time.

### Negative

- Introduces a new runtime with privileged credentials (cross-account S3
  read) that must be audited and rotated.
- Introduces the first Eshu source where raw payload is intentionally
  discarded. This is a deviation from the content-store-first rule and must
  be documented operationally.
- Adds a new failure class (`state_serial_regression`,
  `unknown_provider_schema`) that operators must learn.

### Risks

- **Secret leakage through unknown schemas.** Mitigated by conservative
  default redaction and explicit `unknown_provider_schema` telemetry.
- **Scan storms** when a large org has hundreds of state files and the
  refresh interval is too tight. Mitigated by the coordinator's claim
  fairness rules and by `If-None-Match` conditional reads.
- **Lineage collisions** across workspaces that share locators by accident.
  Mitigated by storing `lineage_uuid` on every fact and refusing silent
  merges across lineage rotations.
- **Terraform version skew.** State schema evolves. The parser must target a
  specific state schema version range and reject unknown versions explicitly
  rather than best-effort parsing.

---

## Appendix: Implementation Workstreams

### Chunk A: Identity And Contracts

**Scope:** ADR, config schema, fact envelopes, scope identity.

**Likely files:**

- `docs/docs/adrs/2026-04-20-terraform-state-collector.md`
- `docs/docs/deployment/service-runtimes.md`
- `go/internal/scope/*`
- `go/internal/facts/*`

### Chunk B: Reader Stack

**Scope:** `StateSource` interface and local + S3 implementations.

**Likely files:**

- `go/internal/collector/terraformstate/source*.go`
- `go/internal/collector/terraformstate/parser.go`
- `go/internal/collector/terraformstate/redact.go`

### Chunk C: Discovery And Coordinator Integration

**Scope:** graph-backed discovery, overrides, coordinator claim loop.

**Likely files:**

- `go/internal/collector/terraformstate/discovery.go`
- `go/internal/collector/terraformstate/service.go`
- `go/cmd/collector-terraform-state/main.go`

### Chunk D: Terragrunt And Output/Module/Tag Coverage

**Scope:** resolver shim and expanded fact coverage.

**Likely files:**

- `go/internal/collector/terraformstate/source_terragrunt.go`
- `go/internal/collector/terraformstate/facts.go`

### Chunk E: DSL Integration (Cross-ADR)

**Scope:** correlation DSL rule packs consuming state facts. Depends on the
DSL ADR follow-up and the AWS scanner ADR's ARN emission contract.

**Likely files:**

- `go/internal/correlation/rules/terraform_state/`
- `go/internal/correlation/rules/terraform_config_state_drift/`
- `go/internal/correlation/rules/state_to_cloud_arn/`

---

## References

- HashiCorp Terraform S3 backend: state path, workspaces, locking, and IAM
  permission expectations:
  <https://developer.hashicorp.com/terraform/language/backend/s3>
- AWS STS `AssumeRole` API, used for central collector cross-account access:
  <https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html>
- Amazon S3 IAM action reference for `GetObject` and versioned object reads:
  <https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-with-s3-policy-actions.html>
- Amazon EKS IRSA service-account role binding:
  <https://docs.aws.amazon.com/eks/latest/userguide/associate-service-account-role.html>
- Amazon EKS Pod Identity service-account role association:
  <https://docs.aws.amazon.com/eks/latest/userguide/pod-id-association.html>

---

## Recommendation

The platform should ship the Terraform State collector as a dedicated
runtime with a unified `StateSource` abstraction covering local, S3, and
Terragrunt-resolved backends. It should lean on graph-backed discovery with
an operator-declarable seed list, gate runs on upstream Git readiness, and
treat secret redaction as a parser-level invariant rather than a post-hoc
filter.

Shipping this collector turns the graph from "what the code said" into "what
Terraform believes it built," which is the prerequisite for the AWS scanner
collector and for all drift, orphan, and unmanaged-resource correlation that
follows.
