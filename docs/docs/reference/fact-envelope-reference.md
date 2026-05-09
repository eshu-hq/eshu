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
payloads with `schema_version: "1.0.0"`. The first core fact kinds are:

| Fact kind | Purpose |
| --- | --- |
| `documentation_source` | A documentation source such as a Confluence space, Git Markdown repository, Notion workspace, or Backstage docs source. |
| `documentation_document` | One document revision with source ID, document ID, external ID, revision ID, URI, labels, owners, ACL summary, and content hash. |
| `documentation_section` | One bounded section within a document revision, including section identity, ordinal path, excerpt hash, and source refs. |
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

## Plugin manifest

Every OCI-packaged plugin ships with a manifest declaring:

| Field | Purpose |
| --- | --- |
| `plugin_id` | Unique plugin identity, used by allowlist and revocation. |
| `publisher_identity` | Publisher key / org identity. |
| `version` | Plugin semver. |
| `compatible_core_range` | Range of Eshu core versions this plugin targets. |
| `emitted_fact_kinds` | List of fact kinds + supported `schema_version` set per kind. |
| `consumer_contract` | Downstream reducer or query consumer contract the plugin expects. |

Plugins introducing a new fact kind MUST also declare the consumer contract
expected to process it. Unknown fact kinds are never presented as active
platform truth.

### Example manifest

```yaml
plugin_id: com.example.cloud-snapshot
publisher_identity: example-corp
version: 1.4.0
compatible_core_range: ">=2.1.0 <3.0.0"
emitted_fact_kinds:
  - kind: com.example.cloud-snapshot.resource
    schema_versions: ["1.2.0"]
  - kind: com.example.cloud-snapshot.relationship
    schema_versions: ["1.0.0"]
consumer_contract:
  reducer: multi-source-reducer
  phases: ["resource_correlation"]
```

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
