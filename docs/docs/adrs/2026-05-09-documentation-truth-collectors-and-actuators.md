# ADR: Documentation Truth Collectors And External Updater Actuators

**Date:** 2026-05-09
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `../reference/truth-label-protocol.md`

---

## Context

Eshu is a read-only engineering truth platform. It observes source systems,
normalizes evidence, reduces that evidence into queryable graph truth, and
serves CLI, HTTP, and MCP consumers. The core platform must keep that trust
boundary as it expands beyond code, IaC, cloud, and runtime collectors.

Company documentation is another high-value source of engineering evidence.
Confluence, Git-backed Markdown, Notion, Google Docs, Backstage, ADR folders,
runbooks, RFCs, and finance or compliance pages often contain claims about
services, deployment paths, dependencies, ownership, operational constraints,
and historical decisions. Those claims frequently drift away from current
source truth.

The platform needs a way to index documentation sources and compare their
claims against Eshu's existing code-to-cloud graph without turning Eshu core
into a write-capable documentation automation product.

---

## Problem Statement

The platform needs to answer documentation freshness questions such as:

- Which documentation pages mention a service, workload, repository, API,
  cloud resource, deployment path, or owner?
- Which document sections contain claims that can be linked to Eshu entities?
- Which linked claims conflict with current Eshu graph truth?
- Which source evidence proves the conflict?
- How fresh and authoritative is that evidence?

At the same time, the platform must not:

- update Confluence, Notion, Google Docs, Git repositories, Backstage, Jira, or
  any other destination system from Eshu core
- call LLM providers from Eshu core for write workflows
- store customer LLM API keys in Eshu core
- create generated prose as an Eshu truth result
- let documentation override source-of-truth graph evidence

Without a clear boundary, documentation automation can corrupt the core trust
model. A documentation page can be a useful signal, but it is not operational
truth merely because it exists.

---

## Decision

### Keep Eshu Core Read-Only

Eshu core remains a read-only evidence and findings platform.

Eshu may read documentation systems through collectors. It may emit normalized
documentation facts, entity mentions, claim candidates, drift findings,
freshness state, truth labels, and evidence packets. It must not publish,
approve, or mutate documentation destinations.

Write-capable workflows belong to separately deployed services that consume
Eshu APIs.

### Add Documentation Source Collectors To Eshu

Documentation collectors run as Eshu collector families and follow the same
source-observer rule as other collectors:

- observe source truth
- assign source scope and generation
- emit typed facts with provenance
- never write canonical graph truth directly
- never mutate the source documentation system

Initial source families should be designed around a common documentation model
rather than a Confluence-specific internal shape.

The target source families include:

- Confluence
- Git-backed Markdown
- Notion
- Google Docs
- Backstage catalog/docs
- ADR and RFC repositories

The first implementation should start with one documentation source, but the
schema and collector contracts must be source-neutral from the beginning.

### Normalize Documentation Facts

Documentation collectors should emit facts for:

- document source
- document identity
- document version or revision
- page hierarchy and parentage
- section identity
- source ACL or permission summary where available
- labels, tags, and ownership metadata
- outbound links
- source excerpts needed for claim extraction
- entity mentions with provenance
- conservative claim candidates

Claim extraction must start conservatively. The first pass should prioritize
page, section, mention, and entity-link facts before introducing broad
free-form claim extraction.

### Eshu Emits Findings, Not Actions

Eshu may compute documentation drift findings. A finding is read-only evidence
that a documentation claim conflicts with Eshu truth.

A finding may include:

- document and section identity
- original claim text or bounded excerpt
- linked Eshu entity
- conflict status
- documented value
- current Eshu truth value
- truth level
- freshness state
- evidence IDs
- source paths or external references
- confidence and ambiguity notes

Eshu must not emit action instructions such as:

- rewrite this paragraph as a specific sentence
- publish this update
- approve this diff
- notify this person to accept the change

Those decisions belong to external updater actuators.

### First Finding Type: Service Deployment Drift

The first documentation finding type should be `service_deployment_drift`.

This finding compares documentation claims about how a service is deployed
against Eshu's code-to-cloud graph evidence, such as ArgoCD applications,
Kubernetes workloads, Helm charts, Kustomize overlays, image references, and
deployment-source repositories.

This is the right first finding because it is bounded, evidence-backed, and
matches Eshu's existing code-to-cloud strength.

### Evidence Packet API

Eshu should expose stable documentation evidence packets for findings.

The updater service should not assemble write context by issuing arbitrary
graph queries and interpreting private graph schema. Eshu should provide a
documentation-ready evidence packet that contains the bounded evidence needed
to explain a finding.

The packet should include:

- finding identity and version
- document, section, and source identity
- bounded source excerpt
- matched Eshu entity IDs
- current graph truth
- truth label and freshness metadata
- evidence references
- ACL or permission decision inputs where available
- ambiguity and unsupported-capability state

External services may snapshot these packets for audit, but Eshu remains the
producer of the canonical read-side packet.

### External Documentation Updater Actuator

The LLM-driven documentation updater should live outside Eshu core.

The updater owns:

- LLM provider adapters
- customer BYOK configuration
- prompt and writer-mode policy
- style profiles
- deterministic patch planning
- bounded LLM drafting
- verifier pipeline
- diff generation
- approval workflow
- web UI
- destination publisher adapters
- publication audit logs

The updater consumes Eshu findings and evidence packets. It does not decide
source truth independently from Eshu.

### LLMs Write Bounded Replacements

The LLM must not decide what is stale. Eshu findings and updater policy decide
what is stale and what edit scope is allowed.

The LLM may only draft bounded replacement text for an approved patch plan.
The updater must validate the output before rendering a diff or publishing it.

The generation pipeline should be:

```text
Eshu finding
  -> Eshu evidence packet
  -> deterministic patch plan
  -> writer mode and style profile
  -> LLM structured draft
  -> deterministic verifier
  -> semantic verifier
  -> rendered diff
  -> approval or publish policy
```

### Writer Modes Are Governed And Versioned

Writer modes customize expression, not truth detection.

A writer mode may define:

- applicable finding types
- applicable document types
- allowed sections
- allowed operations
- forbidden operations
- required evidence
- prompt template
- style constraints
- output schema
- verifier policy
- approval policy

Writer modes must be versioned and immutable after activation. Editing an
active writer mode creates a new version that returns to draft or dry-run
state.

The initial lifecycle is:

```text
draft -> dry_run -> review_required -> publish_allowed
```

High-risk modes such as finance, ADR, RFC, compliance, and runbook writers
should remain review-required unless operating only on explicitly managed
generated sections.

### Custom Writer Modes Are Allowed With Guardrails

End users may create custom writer modes through the updater UI, but they may
not define new truth detection in prompts.

Custom writer modes consume Eshu findings that already exist. A future custom
finding system may be considered only after the first-party finding and
verification model is mature.

ADR and RFC modes should be append-oriented by default because those documents
often record historical decisions or proposals. Current-state drift does not
make the historical record false.

### Style Profiles Are Explicit Workflows

Style profiles may be configured manually and may later be generated from
selected approved source documents.

Style-profile generation must be an explicit user-triggered workflow:

1. user selects approved source documents
2. updater analyzes style only, not truth
3. updater proposes a style profile
4. user reviews and edits it
5. profile enters dry-run before use

Style profiles must be versioned and auditable.

### Verification Is Mandatory

Prompt quality is not sufficient for correctness. The updater must include
deterministic checks and may add semantic LLM-based checks.

Deterministic verification must check:

- output schema validity
- target section and operation scope
- original text hash
- evidence ID references
- required citations
- forbidden section changes
- unsupported operations
- managed-section policy
- required approval state

Semantic verification may check:

- unsupported claims
- weakened warnings or caveats
- incorrect business conclusions
- style mismatch
- drift-finding mismatch

Semantic verification cannot replace deterministic verification.

### Minimal Persistence In The Updater

The updater should persist only what is needed for audit and replay.

It should store:

- evidence packet snapshot
- evidence packet hash
- edited excerpts
- original text hashes
- generated diff
- writer mode version
- style profile version
- provider adapter version
- verifier versions
- approval and publish events

It should avoid storing:

- full documentation corpora
- unrelated page content
- raw provider responses beyond audit need
- secrets, tokens, credentials, or private comments

### Permissions Must Check Both Sides

The updater must enforce both documentation permissions and Eshu evidence
permissions.

A user must be allowed to view the target document, view the supporting Eshu
evidence, approve the writer mode, and publish to the destination before the
UI can show or apply a diff.

Approval routing should start with service and document ownership intersection.
When there is no overlap, policy should require at least one owner from each
relevant side.

### Web UI Is The Approval System Of Record

The external updater needs a web UI because documentation freshness is a
workflow problem, not only an API problem.

The first UI should focus on:

- source inventory
- drift inbox
- evidence view
- diff review
- writer mode administration
- style profile administration
- approval routing
- audit history

External integrations such as Slack, Jira, Confluence comments, and email may
notify and deep-link into the UI. The UI remains the canonical approval and
audit surface for the initial product.

### Self-Hosted First

The updater should be self-hosted first and designed for a future hybrid model.

Self-hosting keeps documentation excerpts, evidence snapshots, OAuth tokens,
LLM API keys, and publication permissions under customer control.

---

## Rejected Alternatives

### Put LLM Write Workflows Inside Eshu Core

Rejected because it weakens Eshu's read-only trust boundary and forces Eshu to
own write permissions, LLM keys, approval workflows, and destination mutation.

### Let The LLM Decide What Is Stale

Rejected because accurate source evidence does not guarantee accurate diffs.
Staleness detection must come from Eshu findings and deterministic policy.

### Start With General Documentation Rewriting

Rejected because broad prose rewriting can corrupt historical context,
operational warnings, roadmap intent, or compliance language. The first finding
type should be bounded and evidence-backed.

### Use Confluence-Specific Models In Core

Rejected because the long-term platform needs many documentation source
collectors. Confluence can be the first source, but the core model must be
source-neutral.

### Auto-Publish Everywhere

Rejected because publication safety depends on section type, writer mode,
evidence strength, document class, ownership, and customer policy.

---

## Consequences

### Positive

- Preserves Eshu's read-only trust boundary.
- Allows documentation sources to become first-class evidence inputs.
- Keeps LLM and write concerns outside Eshu core.
- Creates a reusable documentation fact model for multiple source systems.
- Makes documentation drift findings auditable and evidence-backed.
- Supports self-hosted customer control over tokens, document excerpts, and
  write permissions.

### Negative

- Requires a stable Eshu evidence-packet API before the updater can be simple.
- Introduces a new collector family and source-neutral documentation schema.
- Requires ACL and ownership modeling earlier than a basic content crawler
  would.
- Splits delivery across Eshu core and at least one external actuator service.

### Risks

- Claim extraction can overreach if the first implementation tries to parse all
  prose as truth.
- Documentation ACLs can leak sensitive evidence if the updater does not check
  both document and Eshu permissions.
- Writer modes can become unauditable if mutable active prompts are allowed.
- Auto-publish can damage high-risk document classes unless restricted to
  managed sections and strict policies.

---

## Initial Rollout

1. Add documentation-source collector architecture and fact schema.
2. Implement one source collector, starting with Confluence or Git Markdown.
3. Emit page, section, mention, ownership, and conservative claim-candidate
   facts.
4. Implement `service_deployment_drift` findings in Eshu.
5. Expose stable evidence packet APIs for documentation findings.
6. Build the external updater MVP against Eshu APIs.
7. Keep the first updater mode review-required.

The first demo should show one precise flow:

```text
Documentation says service A deploys through X.
Eshu proves service A deploys through Y.
The updater generates a bounded, style-matched diff.
An owner reviews the evidence and approves or rejects the change.
```

---

## Validation Requirements

Eshu-side implementation must include:

- fixtures for positive, negative, and ambiguous documentation claims
- tests proving documentation facts never override graph truth
- tests proving findings include truth labels and freshness state
- tests proving unsupported or stale evidence is surfaced explicitly
- API contract tests for evidence packets
- telemetry for collector sync, extraction, finding generation, and API latency

Updater-side implementation must include:

- structured-output schema tests
- deterministic verifier tests
- original-text hash mismatch tests
- unsupported-claim rejection tests
- ACL denial tests
- writer-mode version immutability tests
- approval and audit-log tests
