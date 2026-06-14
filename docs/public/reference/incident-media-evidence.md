# Incident Media Evidence Contract

Incident media evidence covers postmortem recordings, transcript files,
screenshots, architecture diagrams, runbook diagrams, and incident-review
exports that may mention services, repositories, workloads, cloud resources,
deployments, or incidents.

This contract governs correlation only. It does not enable media extraction,
OCR, transcription, provider calls, runtime knobs, API routes, MCP tools, graph
writes, or hosted chart behavior.

## Truth Boundary

Media-derived content is documentation evidence. It is not canonical graph truth.

```text
media artifact -> bounded extraction -> documentation facts
  -> mention candidates -> reducer comparison -> query truth label
```

Extraction can prove that a source artifact contained text, labels, links, OCR
regions, or transcript chunks at a specific revision. It cannot prove service
ownership, deployment state, incident root cause, blast radius, cloud inventory,
or current runtime health without deterministic corroboration from source facts,
incident facts, deployment evidence, or cloud/resource facts.

No-provider and default-off modes are supported states. Missing OCR,
transcription, vision, or semantic provider output must produce unavailable,
unsupported, or missing-evidence status. It must not create fallback mentions,
guessed graph edges, or low-authority answers that look complete.

## Source Classes

Incident media evidence uses closed source classes so later reducers and reads
can preserve provenance without mixing extraction confidence with graph truth.

| Source class | Examples | Admissible output |
| --- | --- | --- |
| `transcript_chunk` | Local transcript section from postmortem audio or video | Documentation section, mention candidate, warning classes |
| `ocr_region` | Text region from screenshot or scanned incident artifact | Documentation section, mention candidate, confidence bucket |
| `diagram_label` | Label, node text, or safe link from architecture/runbook diagram | Documentation section, mention candidate, explicit diagram provenance |
| `incident_export_text` | Text from exported incident review notes or attached postmortem summary | Documentation section, mention candidate, evidence handle |

These classes may produce `documentation_entity_mention` or
`documentation_claim_candidate` rows only when the owning extraction contract
allows them. Ambiguous subjects must remain mentions with
`resolution_status=ambiguous` or `unmatched`.

Optional governed `semantic.documentation_observation` rows are separate
provider provenance over already-extracted, policy-allowed media sections. They
may reference a media source class and carry policy, redaction, prompt, model,
confidence, freshness, and admission metadata, but they do not emit
deterministic mentions or claim candidates. A semantic observation without
deterministic corroboration remains provenance-only.

## Correlation States

Correlation compares a media-derived mention with existing deterministic state.
It never promotes the media source by itself.

| State | Meaning |
| --- | --- |
| `exact` | One existing deterministic entity matches the mention and the source class is allowed for review evidence. |
| `corroborated` | Media evidence agrees with deterministic incident, deployment, service, repository, workload, or cloud evidence. |
| `ambiguous` | More than one deterministic entity could match; no winner is chosen. |
| `unmatched` | No existing deterministic entity matches the mention. |
| `stale` | The media artifact, extracted section, or target deterministic state is older than the accepted freshness window. |
| `redacted` | The useful source value was removed or fingerprinted before correlation. |
| `unsupported` | The source format, provider state, language, diagram type, or extraction family is not modeled. |
| `rejected` | The value is unsafe, outside scope, missing ACL proof, or violates extraction policy. |

Only `exact` and `corroborated` states may be presented as evidence beside an
existing graph entity. They still remain documentation evidence unless a later
reducer rule admits a canonical relationship from deterministic facts.

## No-Fabrication Rules

Incident media evidence must not create or overwrite:

- `Service`, `Repository`, `Workload`, `CloudResource`, `Deployment`, or
  `Incident` nodes
- deployable-unit, code-call, ownership, dependency, incident-to-service, or
  cloud-resource relationships
- incident root cause, impact, severity, ownership, remediation, or health
  conclusions
- freshness, completion, queue, or runtime-health claims

If a transcript says "checkout caused the outage", Eshu may store the bounded
source excerpt as documentation evidence. It must not mark the checkout service
as the incident root cause unless deterministic incident or change evidence
later proves that outcome through a separate admission rule.

## Redaction, ACL, And Retention

All media-derived facts inherit the source artifact ACL. Evidence reads must
check the same source permission before returning transcript text, OCR text,
diagram labels, semantic observations, or source excerpts.

Persisted metadata must prefer:

- stable source IDs and content hashes
- redaction version and policy state
- confidence buckets rather than raw provider scores when exact values are not
  needed
- source-relative anchors such as page, region, element, or time range
- warning classes and counts for skipped sensitive regions

Do not persist raw media bytes, rendered frames, image pixels, audio samples,
provider prompts, provider responses, credential handles, personal attendee
lists, private meeting names, real incident titles, raw endpoint URLs, or
operator-local paths in public docs, logs, metric labels, or durable facts.

## Required Fixtures

Implementation PRs must use synthetic fixtures. They must not use real
recordings, production screenshots, private incident exports, private
organization names, private service names, real account identifiers, provider
keys, credentials, personal data, or machine-specific paths.

| Fixture | Required assertion |
| --- | --- |
| Postmortem transcript mentions one known service | Mention resolves to `exact` evidence beside the existing service, not new graph truth. |
| Transcript names two same-label services | Result stays `ambiguous` and lists candidates without choosing by order. |
| Screenshot contains a secret-like value | Sensitive region is redacted or rejected and correlation omits the raw value. |
| Diagram links service to cloud resource | Label evidence remains documentation provenance until deterministic deployment or cloud facts corroborate it. |
| Stale postmortem references current service | Freshness state stays visible and does not overwrite current deterministic state. |
| Unsupported media or missing provider | Returns unsupported or unavailable evidence; no fallback mention is fabricated. |
| Permission-denied artifact | Content is hidden and counts/status remain bounded. |
| Semantic observation without deterministic match | Observation remains provenance-only and does not increment admitted finding counts. |

## Verification Gate

Docs-only changes to this contract run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Verification evidence must include a targeted sensitive-marker scan over every
changed public doc and navigation file. Investigate any output before merge.

Implementation PRs add:

- failing regression tests first for the targeted media source class
- extraction, redaction, ACL, and fixture tests for positive, negative,
  ambiguous, stale, redacted, unsupported, and permission-denied cases
- reducer, API, and MCP tests for every surfaced correlation state
- `scripts/test-verify-collector-authoring-gate.sh`
- `scripts/verify-collector-authoring-gate.sh`
- `scripts/test-verify-performance-evidence.sh`
- `scripts/verify-performance-evidence.sh`
- status, log, trace, and metric evidence for extraction attempts, failures,
  redaction, ACL decisions, provider policy, and correlation outcomes
- performance evidence for extraction wall time, bytes read, decoded bytes,
  media duration, section count, queue depth, and query cost
- security review before enabling production collector, hosted, or chart paths

No-Regression Evidence: this contract is documentation-only. It changes no
extractor, provider, parser, reducer, graph, queue, API, MCP, CLI, or hosted
runtime behavior.

No-Observability-Change: this contract adds no runtime behavior. Future
implementation PRs must name or add operator-visible status, log, metric, and
trace signals for the media evidence path.
