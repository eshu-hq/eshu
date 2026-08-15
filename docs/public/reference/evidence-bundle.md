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

The default export is offline and provider-free. It exercises the stable bundle
schema and validation canaries with deterministic fixture contents for:

- an Ask Eshu answer packet summary;
- a pre-change impact answer packet summary;
- a supply-chain investigation packet summary;
- capability catalog and surface inventory snapshots.

Export what your own stack actually indexed:

```bash
eshu evidence bundle export --live --out evidence-bundle.json
```

`--live` composes the same schema from the running stack's status endpoints
rather than from fixtures, so the bundle reports real repository counts, queue
and generation state, stage and domain backlogs, collector readiness, and
semantic-provider posture. It needs no LLM or provider credential; a stack with
no provider configured reports that as a state rather than failing.

Per-kind fact counts are deliberately absent. No status endpoint exposes them,
so the bundle records a `fact_counts` entry under `missing_evidence` instead of
omitting the gap silently.

See [First Successful Run](../getting-started/first-successful-run.md#prove-the-stack-is-working-with-an-evidence-bundle)
for which fields to read after your first `eshu first-run` to confirm the
stack actually indexed something.

## HTTP API

`GET /api/v0/evidence/bundle` composes the same live artifact `eshu evidence
bundle export --live` produces, from the same status providers, so the console
and the CLI generate the identical bundle rather than two independently
maintained readings. See
[Live Evidence Bundle](http-api/evidence-and-supply-chain.md#live-evidence-bundle)
for the route contract. The route is stack-wide like `--live`, so it accepts
no scope selector and carries no scoped-token support.

## Console

Admin -> Evidence bundle calls the same `GET /api/v0/evidence/bundle` route,
so the console renders the identical artifact the CLI's `--live` export
produces. The panel shows repository count, health, queue state, semantic
provider posture, and any `missing_evidence` rows, and offers the full
artifact as a JSON download (`evidence-bundle.json`) an operator can attach to
a support ticket.

Because the route is stack-wide, it is rejected for a scoped-bearer token and
for a browser session that is not this deployment's tenant-bound all-scopes
owner session — in a hosted multi-tenant deployment that includes every
browser session, even an admin one. When that happens the panel explains the
rejection instead of showing a blank or generic error, and points to the CLI
or a shared/admin token as the alternative path.

The panel does not restate `redaction.rules` or `validation.status` as a
safety guarantee — see [Redaction](#redaction) below for what those fields
actually mean.

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
| `contents.pipeline_state` | Live mode only. Repository count, health, queue and generation state, stage and domain backlogs, and collector readiness. |
| `contents.semantic_provider_state` | Live mode only. Semantic-extraction and provider posture, kept separate from `pipeline_state` so deterministic truth is never presented as provider status. |
| `missing_evidence` | Explicit gaps that prevent overconfident interpretation. |
| `reproduce` | Bounded CLI, API, and MCP calls that can regenerate evidence when the source system is available. |
| `bounds` | Caps and truncation state for bundled layers. |
| `validation` | Schema, redaction, canary, and reproduce-handle checks. |

## Redaction

Bundles carry handles and route/tool/command names, not raw private data.
Validation screens for, and rejects, these known shapes:

- private endpoints;
- credentials, tokens, passwords, and private-key material;
- raw prompts, provider responses, or prompt transcripts;
- local absolute paths under a known filesystem root, and bare `host:port`
  endpoints of the kind Go network errors report, including `.cluster.local`
  service names and private or link-local addresses written without a port;
- every sensitive shape in the shared hosted-governance redaction registry,
  checked against the same canary set the rest of the platform uses.

Screening is pattern-based, so it recognises known shapes rather than proving
their absence. That is why `redaction.rules` names what was screened
(`screened_private_endpoints`) instead of asserting an outcome
(`no_private_endpoints`): treat a passing bundle as screened, not as certified,
and review an artifact before sending it outside your organisation.

Two specifics worth stating, because both are easy to assume the other way:

- A private host written straight after a colon **is** screened. A scope handle
  (`repo:db.internal:5432`) and a labelled diagnostic
  (`upstream:db.internal:5432 refused`) put a colon immediately before the host,
  and both rules used to treat that colon as "still part of the previous word"
  and let the host through. They no longer do.
- A **unique-local IPv6 address** written straight after a colon
  (`peer:fd00::1`) is **not** screened, and that is deliberate. Hextets are
  colon-separated, so a rule that accepted a colon before `fd00` would also read
  the middle hextet of a public address such as `2001:db8:fd12::1` as the start
  of a private one and reject it. Written after a space, a bracket, or at the
  start of a value, a unique-local address is screened normally.

`validation.status` is `unvalidated` as built, and the exporter sets `passed`
only after validation actually runs green, recomputing `bundle_id` over the new
content. `bundle_id` is a content hash for
identifying and de-duplicating an artifact, not a seal: validation does not
rehash the body to check it, because a bundle exported by an older version
hashes differently once a field is added, and rejecting it would defeat the
point of a portable artifact.

So `passed` in a body is a statement about the exporter that wrote it, not a
guarantee to whoever reads it: anyone who edits a bundle can recompute the hash. Re-run
`eshu evidence bundle validate --from <file>` on an artifact you received
instead of trusting the `passed` its body carries.

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
