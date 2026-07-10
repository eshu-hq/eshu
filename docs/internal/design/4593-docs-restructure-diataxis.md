# GetEshu Documentation Restructure

Issue: [#4593](https://github.com/eshu-hq/eshu/issues/4593)

First implementation gate: [#5011](https://github.com/eshu-hq/eshu/issues/5011)

## Decision

Adopt a Diataxis-shaped public documentation architecture for GetEshu:
**Get Started**, **Tutorials**, **How-to Guides**, **Concepts**,
**Reference**, and **Operate**. The first-page experience should feel like a
product documentation system for humans, not a repository index or proof
ledger.

The approved direction is captured as a durable repo design asset:
[4593-geteshu-docs-home-mockup.png](assets/4593-geteshu-docs-home-mockup.png).
The mockup defines the first-screen hierarchy, palette, navigation model, and
audience focus for the #5012 implementation slice. Implementation PRs may adapt
spacing and components to `mkdocs-material`, but should preserve this reader
journey.

## Problem

The current documentation is accurate and exhaustive, but it asks a new reader
to navigate implementation contracts, proof gates, service runbooks, generated
reference, and product explanation at the same level. That is good for
maintainers and agents, but it makes the beginner path noisy.

Current navigation has useful skeletons:

- `Home` points to overview, first-run, MCP, local run, and product pages.
- `Run Locally`, `Use Eshu`, `Connect MCP`, `Deploy to Kubernetes`, and
  `Operate Eshu` already map to real user work.
- `Reference` preserves a large set of API, MCP, CLI, environment, telemetry,
  graph, contract, evidence, and language pages.

The problem is placement, not volume. The restructure should keep the complete
reference corpus reachable while moving beginner and task paths ahead of
contract-heavy material.

## Target IA

| Top-level section | Reader question | Initial page families |
| --- | --- | --- |
| Get Started | "How do I get one useful run?" | `start-here.md`, first successful run, console first five minutes, first questions, local path chooser |
| Tutorials | "Can you teach me by doing?" | First run, vulnerable dependency trace, MCP assistant setup, repo indexing, Kubernetes deploy, stale-answer debugging |
| How-to Guides | "How do I complete this task?" | Index repositories, ask code questions, trace infrastructure, starter prompts, relationship graphs, bundles, CI/CD |
| Concepts | "How does Eshu work?" | Architecture, graph model, modes, service workflows, evidence and truth labels |
| Reference | "What is the exact contract?" | API, MCP, CLI, env vars, generated registries, fact kinds, payload schemas, language support, capability catalogs |
| Operate | "How do I run this safely?" | Health checks, telemetry, freshness, tuning, troubleshooting, hosted operations, deployment runtimes |

`Project` remains for roadmap, contributing, releases, license, and
agent-assisted coding. It should not be part of the first-run path.

## Audience Split Rules

1. Human onboarding docs lead with a task, expected result, and next action.
   They can link to reference pages but should not embed proof-system detail.
2. Tutorials teach a complete outcome in sequence: prerequisites, steps,
   expected result, failure hints, and read-next links.
3. How-to guides solve one task without teaching the whole product model.
4. Concepts explain durable mental models, not command catalogs.
5. Reference pages are exhaustive, generated when possible, and organized for
   lookup rather than first reading.
6. Operator pages stay visible under **Operate** because production safety is a
   human path, not maintainer-only material.
7. Maintainer, agent, proof, and contract material is preserved, searchable,
   and linkable, but it should leave beginner nav unless it is needed for a
   specific user task.
8. Generated pages must carry a generated marker, name their source of truth,
   and have a local/CI drift gate before they are treated as canonical.

## Representative Page Mapping

| Existing page or family | Target section | Notes |
| --- | --- | --- |
| `docs/public/getting-started/first-successful-run.md` | Get Started, Tutorials | Keep as the first concrete outcome. Tutorial wrapper may link to it. |
| `docs/public/start-here.md` | Get Started | Becomes the path chooser after the home page. |
| `docs/public/run-locally/index.md` | Get Started | Keep local setup visible, but route through one beginner path. |
| `docs/public/mcp/index.md` | Get Started, How-to Guides | Human assistant connection path; detailed tool contracts stay in Reference. |
| `docs/public/use/index.md` and `docs/public/guides/*.md` | How-to Guides | Keep only task-oriented pages prominent. |
| `docs/public/concepts/*` and `docs/public/architecture.md` | Concepts | Explanation pages should not compete with tutorials. |
| `docs/public/operate/*` and `docs/public/deployment/service-runtimes*.md` | Operate | Production safety remains a top-level human path. |
| `docs/public/deploy/kubernetes/*` and `docs/public/deployment/*` | Operate, How-to Guides | Quick deploy tasks can be guides; runtime detail belongs under Operate. |
| `docs/public/reference/http-api*` | Reference | Generated or source-checked from `go/internal/query/openapi*.go`. |
| `docs/public/reference/mcp-*` | Reference | Generated or source-checked from MCP schema and capability inventory. |
| `docs/public/reference/env-registry.md` | Reference | Generated or source-checked from the environment registry. |
| `docs/public/reference/*evidence*`, proof gates, cassettes, and replay docs | Reference or Project proof area | Preserve, but remove from beginner choices unless a task needs them. |
| `AGENTS.md`, `CLAUDE.md`, `.agents/skills/`, package `AGENTS.md` | Not public nav | Agent and maintainer rules stay out of human docs IA. |
| `docs/internal/*` | Not public nav | Internal design and maintenance artifacts stay internal. |

## Page Templates

### Tutorial

- Goal and concrete outcome.
- Time and prerequisites.
- Steps in order.
- Expected output or success signal.
- Failure hints for common wrong states.
- Read next.

### How-to

- Task statement.
- Preconditions.
- Minimal steps.
- Verification.
- Related references.

### Concept

- Problem the concept solves.
- Mental model.
- How it appears in Eshu.
- Links to tutorials, guides, and reference.

### Reference

- Source of truth.
- Generated marker when generated.
- Exact command, route, schema, field, environment variable, or contract.
- Drift check or owner.
- Related operational notes.

### Operation

- Operational objective.
- Signals to inspect.
- Normal and degraded states.
- Recovery or escalation steps.
- Telemetry, logs, metrics, and linked references.

## Migration Order

1. #5011: land this design gate and approved mockup artifact.
2. #5012: reshape the public home page and top-level nav to the approved IA.
3. #5013: add the tutorial landing page and first tutorial set.
4. #5014: move proof, maintainer, agent, and contract-heavy pages out of the
   beginner path without deleting them.
5. #5015: add lightweight docs metadata and a catalog check for entrypoints.
6. #5017: generate reference families from source truth and enforce drift
   checks.
7. #5016: add the prose-quality gate in advisory mode, then make it blocking
   after the baseline is clean.
8. #5018: publish the public roadmap page and link the adoption-facing epics.

## Follow-up Acceptance Checks

### #5012 homepage and nav

- Home page routes new users, assistant users, deployers, and operators to
  distinct first actions.
- Top-level nav exposes Get Started, Tutorials, How-to Guides, Concepts,
  Reference, and Operate.
- Existing high-value links remain reachable.
- Strict MkDocs build passes.

### #5013 tutorials

- Tutorial landing page exists and is linked from home/nav.
- At least four tutorial entries route to current docs or thin wrappers.
- Tutorial pages include expected results, not commands alone.

### #5014 audience split

- Proof artifacts are not first-run choices.
- Maintainer/proof/reference pages remain reachable from Reference, Project,
  or a named proof area.
- No docs are deleted because they are too detailed.

### #5015 metadata and catalog

- Metadata schema documents page type, audience, and entrypoint rules.
- Initial landing/entrypoint pages carry metadata.
- The check fails on missing files, bad type values, and entrypoints that are
  not reachable from a landing page.

### #5017 generated reference

- OpenAPI, MCP, and environment reference families are generated or checked
  from repo truth.
- Generated pages carry markers and documented commands.
- Drift gates fail when generated output is stale.
- Fact-kind pages use the registry v2 source from #4570.
- Payload schema pages use the factschema source from #4567.

### #5016 prose gate

- Advisory check covers the human-facing subset first.
- Generated, proof, and reference exceptions are explicit.
- The blocking switch is tracked and does not surprise legacy docs.

### #5018 roadmap

- Roadmap has now, next, and later sections.
- It links durable issue truth instead of private plans or delivery dates.
- It explains how first-run, generated reference, docs IA, contract work, and
  conformance fit together.

## Dependencies

Generated fact-kind and payload reference work originally depended on:

- [#4570](https://github.com/eshu-hq/eshu/issues/4570) for fact-kind registry
  v2 payload schema references and deprecation markers.
- [#4567](https://github.com/eshu-hq/eshu/issues/4567) for `sdk/go/factschema`
  schema generation.

Both issues are closed in current GitHub issue state. #5017 should still verify
the landed source files and generators before claiming generated fact-kind or
payload reference completion.

## Non-goals

- Replace `mkdocs-material`.
- Move the whole corpus in one PR.
- Rewrite `AGENTS.md`, `CLAUDE.md`, project skills, or package-local agent
  instructions.
- Delete proof or reference docs simply because they are too detailed.
- Introduce generated reference pages without source-of-truth checks.

## Verification For This Design Slice

This design-only slice changes internal docs and static design assets. Required
verification:

```bash
git diff --check
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```
