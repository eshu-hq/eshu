# Portable Evidence Bundle

`evidence_bundle.v1` is a share-safe snapshot for support, issue handoff, and
operator debugging. It packages selected answer packet summaries, investigation
packet summaries, capability catalog handles, surface inventory handles,
freshness/readiness state, missing evidence, and reproduce calls into one JSON
artifact.

It is not a graph export, database backup, raw source archive, trace dump, or
provider transcript.

## CLI

Export a deterministic bundle:

```bash
eshu evidence bundle export --scope repo:demo/service --out evidence-bundle.json
```

Validate a bundle before sharing:

```bash
eshu evidence bundle validate --from evidence-bundle.json
```

The first implementation is offline and provider-free. It exercises the stable
bundle schema and validation canaries with deterministic fixture contents for:

- an Ask Eshu answer packet summary;
- a pre-change impact answer packet summary;
- a supply-chain investigation packet summary;
- capability catalog and surface inventory snapshots.

## Shape

The top-level artifact contains:

| Field | Meaning |
| --- | --- |
| `schema_version` | Always `evidence_bundle.v1`. |
| `bundle_id` | Deterministic content ID for the redacted bundle. |
| `identity` | Share-safe scope, profile, and fixture creation timestamp. |
| `source` | Redacted repository/deployment handles. |
| `redaction` | Share-safe profile and applied rules. |
| `contents` | Answer packets, investigation packets, catalog snapshots, and operator state. |
| `missing_evidence` | Explicit gaps that prevent overconfident interpretation. |
| `reproduce` | Bounded CLI, API, and MCP calls that can regenerate evidence when the source system is available. |
| `bounds` | Caps and truncation state for bundled layers. |
| `validation` | Schema, redaction, canary, and reproduce-handle checks. |

## Redaction

Bundles carry handles and route/tool/command names, not raw private data.
Validation rejects:

- private endpoints;
- credentials, tokens, passwords, and private-key material;
- raw prompts, provider responses, or prompt transcripts;
- local absolute paths;
- raw source blobs or private source excerpts.

If a source cannot provide a share-safe value, the bundle should keep an explicit
missing-evidence or redaction reason instead of deleting the row silently.

## Relationship To Other Artifacts

An investigation evidence packet explains one investigation in detail. An
operator digest summarizes one scope for human handoff. An evidence bundle
packages multiple proof surfaces together so the same support ticket can carry
answer, packet, catalog, freshness, missing-evidence, and reproduce handles.

The bundle does not replace investigation packets. It references or summarizes
them with enough handles to reproduce bounded calls when the source deployment is
available.
