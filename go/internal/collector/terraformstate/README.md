# Terraform State Collector

## Purpose

`internal/collector/terraformstate` owns the first Terraform-state collection
primitives. It resolves exact state candidates, opens exact state sources,
parses state with a streaming JSON decoder, redacts values before they cross the
parser boundary, and emits Terraform-state fact envelopes.

This package does not schedule collector runs, write graph rows, persist raw
state, or call cloud APIs directly. Coordinator claims, reducer projection, and
AWS SDK wiring belong to integration slices outside the reader stack.

## Current Surface

- `StateSource` opens one exact Terraform state stream.
- `LocalStateSource` reads an operator-approved absolute file path only.
- `S3StateSource` wraps a caller-supplied read-only object client and sends an
  exact bucket/key request with optional `If-None-Match` and version metadata.
- `DiscoveryResolver` turns explicit seeds, Git-observed backend facts, and
  explicitly approved Git-local state candidates into exact `StateKey`
  candidates without opening raw state.
- Git HCL parsing emits `terraform_backends` metadata for Terraform `backend`
  blocks. The Postgres adapter reads those facts from active Git generations
  and only returns exact S3 candidates with literal bucket, key, and region
  values.
- Git HCL parsing also emits `terragrunt_remote_states` metadata for
  Terragrunt `remote_state` blocks, including blocks resolved through nested
  `include` chains. `TerragruntRemoteStateCandidate` translates each row
  into a `DiscoveryCandidate` carrying the underlying backend kind (`s3` or
  `local`); discovery never observes `BackendTerragrunt`.
- The Git collector may emit `terraform_state_candidate` facts for repo-local
  `.tfstate` files. Those facts are metadata only: repo ID, repo-relative path,
  path hash, size, and warning flags. They do not include raw state bytes or
  absolute filesystem paths.
- The Postgres readiness adapter reports graph discovery as ready only when the
  upstream Git repository fact is tied to an active committed generation.
- `ParseDiscoveryConfig` maps collector-instance JSON into the typed discovery
  config used by the resolver.
- Discovery candidates carry `target_scope_id` when config or approval policy
  supplies one. The reader stack treats it as routing metadata; source opening
  code decides which cloud credentials to use.
- `NewDiscoveryMetrics` registers the candidate counter used during discovery.
- `Parse` turns one state stream into redacted Terraform-state facts.
- Parser results include bounded operational stats for resource facts and
  redactions by reason. Runtime code records those as metrics; raw values and
  source locators stay out of labels.
- `ReadSnapshotIdentity` streams only the top-level serial and lineage fields so
  runtime code can build the claimed generation identity without retaining raw
  state bytes.
- `ParseOptions` carries scope, generation, source, fencing, and redaction
  context.
- `internal/collector/tfstateruntime` adapts these primitives to workflow
  claims: it resolves exact candidates, opens a matching source, parses facts
  with the claim fencing token, and leaves SDK-specific cloud wiring behind the
  existing read-only source interfaces.
- `cmd/collector-terraform-state` supplies the current AWS SDK adapter for
  read-only S3 access in the claim-driven runtime.

## Safety Rules

- Raw state bytes are only allowed in the source reader and parser window.
- Full S3 URLs and local paths are not emitted in facts; parser facts use a
  locator hash in payload and source references.
- Repo-local state discovered by Git is discover-only by default. The
  Terraform-state collector opens it only when `local_state_candidates.mode` is
  `approved_candidates` and the config names an exact repo-relative path. An
  approval may include `target_scope_id`, but a local read does not require one.
- Exact local seeds still require operator-approved absolute paths.
- S3 reads are exact object reads. Prefix-only keys are rejected.
- Graph-backed discovery waits for Git generation readiness before reading
  Terraform backend facts.
- Dynamic backend expressions, workspace-prefixed S3 backends, non-S3 backends,
  and unapproved local paths from Git facts are not discovery candidates.
- S3 write capability is rejected at source construction.
- Redaction key material is mandatory before parsing.
- Unknown provider-schema scalar attributes are redacted. Unknown composite
  attributes are dropped and represented by warning facts.
- `tags` and `tags_all` are emitted as `terraform_state_tag_observation`
  facts for correlation indexing, but scalar tag keys and values still follow
  the unknown provider-schema rule and are redacted by default. Non-scalar tag
  values are dropped and represented by warning facts.
- DynamoDB lock metadata is read-only and observational. The reader records the
  digest and a lock ID hash, but consistency decisions still come from the
  opened state body and durable generation metadata.
