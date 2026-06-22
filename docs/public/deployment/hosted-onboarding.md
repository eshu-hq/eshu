# Hosted Project Onboarding

`eshu hosted-onboard` is the shared-service onboarding workflow for a *deployed*
Eshu service. It lets a platform operator hand a project team a scoped
connection story for API and MCP clients, and a redacted artifact that says
whether ingestion and a first useful answer are ready, without exposing any
secret.

Use it instead of hand-editing Helm `repoSync` values as the primary onboarding
model. For the underlying connection checks it reuses, see
[`eshu hosted-setup`](../reference/cli-reference.md). For the deployment runtime
map, see [Service Runtimes](service-runtimes.md). Before handing the artifact
to a team, use [Hosted Governance Posture](../operate/hosted-governance.md) to
confirm the current auth, semantic-provider, extension, redaction, and proof
boundaries.

## What it does

1. Takes a team name and a **narrow** repository sync rule set.
2. Validates the rules and **rejects an accidental whole-org glob** unless you
   explicitly confirm broad ingestion.
3. Reuses the `hosted-setup` staged connection checks: `/healthz`, `/readyz`
   (which also proves authentication), index readiness, MCP tool visibility, and
   one bounded query.
4. Emits a redacted onboarding artifact that is safe to hand to the team.

The exit code is truthful: it is non-zero unless the bounded first-answer query
actually returned. Process health alone is never reported as success.

## Run it

Resolve the deployed endpoint and bearer token through the shared remote flags
(`--service-url` / `ESHU_SERVICE_URL`, `--api-key` / `ESHU_API_KEY`, then
persisted config), then onboard a team with an explicit, narrow repository set:

```bash
export ESHU_SERVICE_URL=https://eshu.example.com
export ESHU_API_KEY=...   # value is read from the environment, never printed

eshu hosted-onboard \
  --team payments \
  --repo acme/payments-api \
  --repo acme/payments-worker \
  --platform claude \
  --out onboarding-payments.md
```

A scoped prefix pattern is also narrow and is accepted without confirmation:

```bash
eshu hosted-onboard --team payments --repo-pattern '^acme/payments-'
```

Write a JSON artifact instead of Markdown:

```bash
eshu hosted-onboard --team payments --repo acme/payments-api \
  --out onboarding-payments.json --format json
```

## Narrow versus broad repository rules

The workflow classifies the rule set before any connection check runs:

| Rule set | Classification |
| --- | --- |
| One or more `--repo owner/name` exact selectors | Narrow |
| A scoped prefix `--repo-pattern '^org/team-'` | Narrow |
| `--repo-pattern 'org/*'`, `'*'`, `'.*'`, or `'org/.*'` | Broad |
| No rules at all (the deployed `githubOrg` mode would ingest the whole org) | Broad |
| An explicit repo mixed with a broad glob | Broad |

A broad rule set is **rejected** with a non-zero exit unless you pass
`--confirm-broad`. This prevents an accidental org-wide ingestion from a stray
glob. When broad ingestion is genuinely intended, confirm it explicitly:

```bash
eshu hosted-onboard --team platform --repo-pattern 'acme/*' --confirm-broad
```

The artifact always records whether the rule set was broad and whether broad
ingestion was confirmed.

## The artifact

The onboarding artifact is a presentation layer over the reused `hosted-setup`
result. Every endpoint is redacted and only the token **source name** is
recorded. It includes:

- **API URL** and **MCP URL** (`<base>/mcp/message`), with any embedded
  credentials redacted.
- **Token source name** — the `ESHU_API_KEY` environment variable the team
  configures. The token **value is never written** into the artifact.
- **Repository scope** — the validated rules and the narrow/broad/confirmed
  classification.
- **Index state** and **queue/completeness status** derived from the readiness
  verdict (`ready`, `building`, `stale`, `empty`).
- **Indexed repositories** observed by the bounded query.
- **Starter prompts** sourced from the query playbook catalog, so they always
  name first-class tools.
- **Starter playbooks** with `playbook_id`, `version`, `prompt_family`, prompt
  text, ordered tool sequence, and expected answer truth classes.
- **Next steps** tailored to the first failing stage when onboarding is
  incomplete.

The artifact is safe to commit to a team repo or paste into a ticket: it carries
no secret value.

## First answer playbooks

Hosted onboarding gives teams the same playbook-backed starting points as the
public [Starter Prompts](../guides/starter-prompts.md). In the JSON artifact,
read `starter_playbooks[]`; in Markdown, read the **Starter playbooks** section.

Current catalog-backed starters are:

| Need | Playbook | Tool sequence | Expected truth |
| --- | --- | --- | --- |
| Explain and cite a service | `service_story_citation@1.0.0` | `get_service_story` -> `build_evidence_citation_packet` | `deterministic`, then citation `code_hint` |
| Investigate code topic evidence | `repository_code_topic_investigation@1.0.0` | `investigate_code_topic` -> `get_code_relationship_story` | `code_hint`, then `deterministic` |
| Cite a documentation finding | `documentation_truth_citation@1.0.0` | `get_documentation_evidence_packet` -> `check_documentation_evidence_packet_freshness` | `semantic_observation`, then freshness `deterministic` |

Read these like answer packets, not generic assistant prose:

- `truth.freshness.state=building` or `stale` means the answer may be useful but
  not complete; re-run after readiness improves before acting on it.
- `partial=true`, `truncated=true`, or a limitation means the answer is bounded,
  not false. Follow `recommended_next_calls` before claiming full coverage.
- Citation packets hydrate returned handles. If a citation packet is truncated,
  request the next bounded handle batch instead of widening to raw graph access.

There is no dedicated readiness or hosted-governance playbook in the current
catalog. Until one exists, hosted onboarding uses staged readiness checks
(`/healthz`, `/readyz`, index readiness, MCP tools, one bounded query) and the
prompt-ready status tools such as `get_index_status`. Governance prompts must
name the active auth posture instead of implying tenant isolation from a shared
token alone.

## Authorization Boundary (read before you share)

The deployed API and MCP surface can run with a **single shared bearer token**.
That mode is not a tenant boundary: every holder of the token can read every
indexed repository. Treat the token source as a shared-service credential, not
tenant-isolated access.

Scoped per-team tokens are available when the operator mounts an
`ESHU_SCOPED_TOKENS_FILE` registry for API and MCP. The registry stores token
hashes only and maps each token to tenant, workspace, repository, and
source-scope grants. Missing, malformed, or unreadable scoped-token registry
configuration fails closed during service startup.

The onboarding artifact must state which posture is active: shared token,
scoped-token registry, or a future identity-backed token/session model. Do not
present a shared onboarding token as tenant isolation. Use
[Hosted Governance Posture](../operate/hosted-governance.md) and
[User Management Runbook](../operate/user-management-runbook.md) for the
operator preflight, scoped-token lifecycle, and auth proof gates.
