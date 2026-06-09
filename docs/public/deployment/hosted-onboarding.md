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
- **Next steps** tailored to the first failing stage when onboarding is
  incomplete.

The artifact is safe to commit to a team repo or paste into a ticket: it carries
no secret value.

## Authorization limitation (read before you share)

The deployed API and MCP surface authenticate with a **single shared bearer
token**. There is **no per-team or per-repository token scoping today**: every
holder of the token can read every indexed repository. Treat the token source as
a shared-service credential, not a tenant-isolated secret.

The onboarding artifact states this limitation verbatim so it never implies
isolation that does not exist. Scoped per-team tokens are tracked as a follow-up
under the hosted-ops capability ([issue #1852](https://github.com/eshu-hq/eshu/issues/1852)).
Until that lands, do not present the onboarding token as a tenant boundary.
Use [Hosted Governance Posture](../operate/hosted-governance.md) for the
operator preflight and runbooks that keep that limitation explicit.
