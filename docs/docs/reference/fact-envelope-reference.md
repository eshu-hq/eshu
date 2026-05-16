# Fact Envelope And Collector Plugin Reference

This page consolidates the Eshu collector contract: what collectors emit, how
plugins are packaged and trusted, and the compatibility rules that gate plugin
activation. It is the user-facing reference for contributors building a new
collector family.

For the formal specs behind this reference:

- [Fact Schema Versioning](fact-schema-versioning.md)
- [Plugin Trust Model](plugin-trust-model.md)

## Collector contract at a glance

Collectors do two things and only two things:

1. Observe source truth (git repos, Terraform state, Kubernetes manifests,
   Helm, ArgoCD, Crossplane, …).
2. Emit **versioned facts** against the Eshu fact envelope.

Collectors do NOT:

- Write directly to the canonical graph. The reducer and graph-write layer
  own that.
- Apply durable-store DDL migrations. Core runtime owns DDL.
- Synthesize truth from multiple sources. That is reducer ownership.

Why: this seam is the reason OCI-packaged plugins can ship without patching
the core runtime, and the reason adding a new collector family does not risk
corrupting canonical graph state.

## Fact envelope

Every fact carries two identity fields:

- `fact_kind` — the domain kind this fact describes.
- `schema_version` — semantic version of the fact schema.

Core fact kinds are owned by Eshu. Plugin fact kinds must use a
collision-resistant prefix (reverse-DNS recommended) so two plugins cannot
claim the same kind.

Example fact envelope (abbreviated):

```json
{
  "fact_kind": "com.example.cloud-snapshot.resource",
  "schema_version": "1.2.0",
  "scope_id": "state-snapshot://prod/us-east-1/api-gateway",
  "generation_id": "state-serial-417",
  "payload": {
    "resource_id": "aws_api_gateway_rest_api.root",
    "resource_type": "aws_api_gateway_rest_api",
    "attributes": { "...": "..." }
  }
}
```

The full envelope shape is defined by the core Go fact types in
`go/internal/facts/`. Plugins serialize against the shared envelope; they do
not invent their own envelope shape.

## Core documentation fact families

Documentation collectors use the shared envelope and emit source-neutral
payloads. Most core documentation facts use `schema_version: "1.0.0"`;
`documentation_section` uses `schema_version: "1.1.0"` because section payloads
can carry source-native body content for updater diff generation.

| Fact kind | Purpose |
| --- | --- |
| `documentation_source` | A documentation source such as a Confluence space, Git Markdown repository, Notion workspace, or Backstage docs source. |
| `documentation_document` | One document revision with source ID, document ID, external ID, revision ID, URI, labels, owners, ACL summary, and content hash. |
| `documentation_section` | One bounded section within a document revision, including section identity, ordinal path, source-native content, content format, excerpt hash, and source refs. |
| `documentation_link` | One outbound or internal link observed in a document section. |
| `documentation_entity_mention` | One possible mention of an Eshu entity with exact, ambiguous, or unmatched resolution state. |
| `documentation_claim_candidate` | One conservative, non-authoritative claim candidate found in documentation text. |

Documentation facts are evidence about what documentation says. They do not
override source-code truth, deployment truth, runtime truth, or canonical graph
truth. Reducers and documentation drift findings may compare documentation
evidence with graph truth, but documentation facts must not be treated as graph
truth by themselves.

ACL summaries on documentation facts are source evidence, not authorization
decisions by themselves. A source may report only that the collector credential
could view a document while the full page restrictions were not collected. In
that case the payload should mark the ACL summary as partial, and evidence
packet APIs must fail closed unless the packet carries an explicit
`viewer_can_read_source=true` permission decision.

Documentation section payloads may store source-native body content in
Postgres. Confluence uses its storage-body format. Collectors and runtimes must
not emit that content through logs or metrics, and read surfaces must apply the
same evidence permission checks before exposing stored body content.

## Core Terraform State Fact Families

Terraform state facts use `collector_kind: "terraform_state"` and
`schema_version: "1.0.0"` for the first collector contract. The reader must
redact sensitive values before it emits any of these facts.

| Fact kind | Purpose |
| --- | --- |
| `terraform_state_snapshot` | One observed state object with backend metadata, serial, lineage, size, and observation time. |
| `terraform_state_resource` | One resource instance from state, including safe identity fields and redacted attributes. |
| `terraform_state_output` | One named output with its sensitive flag and redacted or digest value. |
| `terraform_state_module` | One module entry with source, version, path, and input digest. |
| `terraform_state_provider_binding` | One provider configuration reference such as alias, region, assume-role ARN, or account hint. |
| `terraform_state_tag_observation` | One tag key/value observation split out for correlation indexing. |
| `terraform_state_warning` | One non-fatal warning such as `state_in_vcs`, `lineage_rotation`, or `serial_regression`. |

`terraform_state_candidate` is the exception to the `collector_kind:
"terraform_state"` rule above. The Git collector emits it as safe metadata for
repo-local `.tfstate` files. Its payload carries the repo ID, repo-relative
path, path hash, size, backend kind, candidate source, and warning flags. It
must not carry raw state content or an absolute local path.

## Compatibility rules

`schema_version` uses [semantic versioning](https://semver.org/). Runtime
behavior on mismatch:

| Bump | Runtime behavior |
| --- | --- |
| Major | If the runtime does not declare support for the emitted major, the runtime rejects the plugin or the emitted fact family. Hard error. No silent fallback. |
| Minor (runtime < plugin) | If the plugin emits a newer minor than the runtime understands, the runtime fails clearly rather than silently accepting unknown fields as authoritative. |
| Minor (runtime ≥ plugin) | Older fact rows with null/missing fields are preserved; the runtime does not invent values. |
| Patch | Must be backward-compatible and must not change semantic meaning. |

In-store migration policy:

- Backward-compatible readers may dual-read old + new schema versions during
  an operator-visible migration window.
- Incompatible schema changes require explicit migration or reindex paths.
- Silent in-place reinterpretation of stored facts is NOT allowed.

## Component package manifest

Every component package ships with a manifest declaring:

| Field | Purpose |
| --- | --- |
| `metadata.id` | Unique component identity, used by allowlist and revocation. |
| `metadata.publisher` | Publisher key / org identity. |
| `metadata.version` | Component semver. |
| `spec.compatibleCore` | Range of Eshu core versions this component targets. |
| `spec.emittedFacts` | List of fact kinds + supported `schema_version` set per kind. |
| `spec.emittedFacts[].sourceConfidence` | Source-confidence values each fact family emits. `unknown` is not allowed for component output. |
| `spec.consumerContracts` | Downstream reducer or query consumer contract the component expects. |

Plugins introducing a new fact kind MUST also declare the consumer contract
expected to process it. Unknown fact kinds are never presented as active
platform truth.

### Example manifest

```yaml
apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: com.example.cloud-snapshot
  name: Cloud snapshot collector
  publisher: example-corp
  version: 1.4.0
spec:
  compatibleCore: ">=2.1.0 <3.0.0"
  componentType: collector
  collectorKinds:
    - cloud_snapshot
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/example/cloud-snapshot@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: com.example.cloud-snapshot.resource
      schemaVersions: ["1.2.0"]
      sourceConfidence: ["reported"]
    - kind: com.example.cloud-snapshot.relationship
      schemaVersions: ["1.0.0"]
      sourceConfidence: ["reported"]
  consumerContracts:
    reducer:
      phases: ["resource_correlation"]
```

## Core AWS Cloud Fact Families

AWS cloud scanner facts use `collector_kind: "aws"` and
`source_confidence: "reported"` because the AWS APIs report live account
state. The facts package currently defines:

| Fact kind | Purpose |
| --- | --- |
| `aws_resource` | One AWS resource reported by a service API. |
| `aws_relationship` | One relationship reported by AWS APIs. |
| `aws_tag_observation` | One raw AWS tag observation when split out for correlation indexing. |
| `aws_dns_record` | One Route 53 DNS record observation. |
| `aws_image_reference` | One ECR image digest/tag reference. |
| `aws_warning` | One non-fatal AWS scanner warning. |

Reducers must corroborate workload, deployment, ownership, and environment truth
before AWS evidence is promoted into canonical graph answers.

## Core CI/CD Run Fact Families

CI/CD run facts use `collector_kind: "ci_cd_run"` and
`source_confidence: "reported"` when provider runtime metadata reports the run,
job, artifact, trigger, environment, or warning evidence. The first
implementation slice is fixture-backed for GitHub Actions and does not poll
hosted APIs or manage credentials.

| Fact kind | Purpose |
| --- | --- |
| `ci.pipeline_definition` | One provider workflow or expanded runtime definition with workflow ID/path, name, state, trigger, run identity, and repository anchors. |
| `ci.run` | One provider run with run ID, attempt, event, status/result, branch, commit SHA, repository locator, actor, timestamps, and URL metadata. |
| `ci.job` | One job under a run with provider job ID, name, status/result, runner labels, and timing metadata. |
| `ci.step` | One ordered step with provider step number, name, action reference when present, status/result, and timing metadata. |
| `ci.artifact` | One provider artifact metadata row with provider artifact ID, name, type, size, digest when reported, expiration, and tokenless download reference. |
| `ci.trigger_edge` | One explicit provider trigger relation, such as workflow-call or upstream-run evidence. |
| `ci.environment_observation` | One provider environment observation with environment name and deployment/status metadata. |
| `ci.warning` | One non-fatal provider or fixture warning, such as partial job metadata or a missing artifact digest. |

Provider-native IDs and run attempts are part of fact identity so retries remain
separate. Artifact download URLs with query strings are stripped. CI success,
environment names, and shell text remain provenance evidence; reducers must
corroborate them with stronger artifact, deployment, runtime, or graph truth
before promotion.

## Trust model

Plugins are untrusted by default. Activation requires operator configuration.

### Activation modes

| Mode | Meaning |
| --- | --- |
| `disabled` | All plugins ignored. |
| `allowlist` | Only explicitly approved plugin identities may load. |
| `strict` | Allowlist + signature / provenance verification required. |

### Verification checks

Before activation, the runtime MUST verify:

- Artifact identity matches the allowlisted plugin ID.
- Artifact provenance (preferred signing model: Sigstore / Cosign-compatible
  OCI artifact signatures).
- Fact schema compatibility (via manifest + envelope checks above).
- Operator allowlist or equivalent trust policy admits this publisher.

### Failure policy

- Incompatible or untrusted plugins fail closed.
- Failure logs identify the plugin and the violated rule.
- Operators may choose whether one plugin failure blocks startup entirely.
- Publisher identity rotation requires an explicit trust-transfer procedure.
  Silent key replacement is not allowed.

## Idempotency invariant

Facts MUST be idempotent under at-least-once delivery. Emitting the same fact
twice must not create divergent truth. Plugin authors are responsible for
enforcing this invariant in their emission code.

## Bump decision tree

When changing a fact schema, ask:

1. Does the change remove or semantically redefine an existing field? →
   **major bump**, requires explicit core support update.
2. Does the change add a field that readers can safely ignore? →
   **minor bump**, requires declared core compatibility.
3. Is the change a doc fix, bug fix with no semantic effect, or non-semantic
   correction? → **patch bump**.

When in doubt, bump higher. A conservative bump is cheap. A silent semantic
change destroys downstream correctness.

## Deprecation window

Unsupported major versions are not removed abruptly. The compatibility window
for a given major MUST be documented at release time so operators have a
predictable upgrade path.

## Related

- [Fact Schema Versioning](fact-schema-versioning.md)
- [Plugin Trust Model](plugin-trust-model.md)
- [Architecture — Collector Extensibility And OCI Plugins](../architecture.md#collector-extensibility-and-oci-plugins)
- [Capability Conformance Spec](capability-conformance-spec.md)
