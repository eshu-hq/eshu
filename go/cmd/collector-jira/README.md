# collector-jira

## Purpose

`collector-jira` is the hosted Jira work-item evidence collector. It selects an
enabled, claim-capable `jira` collector instance from
`ESHU_COLLECTOR_INSTANCES_JSON`, claims Jira site work, fetches bounded updated
windows from Jira Cloud, and commits `work_item.*` source facts.

## Ownership boundary

This command wires the Jira source package to workflow claims, runtime admin,
telemetry, and Postgres fact commits. It does not perform incident, deployment,
code, pull-request, graph, or read-model correlation.

Signed Jira webhooks can wake the same configured `scope_id` through
`incident_freshness_triggers`, but the webhook listener does not emit
`work_item.*` facts. The workflow coordinator must authorize the trigger
against this collector configuration and create normal Jira collector work, and
polling remains the backfill path for missed webhook deliveries.

## Exported surface

The command exposes the `collector-jira` binary and these environment
variables:

| Variable | Purpose |
| --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances with one enabled `jira` instance. |
| `ESHU_JIRA_COLLECTOR_INSTANCE_ID` | Required when more than one enabled Jira instance exists. |
| `ESHU_JIRA_POLL_INTERVAL` | Delay between empty claim polls. Defaults to `1s`. |
| `ESHU_JIRA_CLAIM_LEASE_TTL` | Lease TTL for workflow claims. |
| `ESHU_JIRA_HEARTBEAT_INTERVAL` | Heartbeat interval; must be less than the lease TTL. |
| `ESHU_JIRA_COLLECTOR_OWNER_ID` | Optional claim owner label. |

Each target inside `ESHU_COLLECTOR_INSTANCES_JSON` names credentials with
`token_env` and optional `email_env`. The runtime resolves those variables at
startup and never persists the resolved values.

Target shape:

```json
{
  "provider": "jira_cloud",
  "scope_id": "jira:site:example",
  "site_id": "example.atlassian.net",
  "base_url": "https://example.atlassian.net",
  "email_env": "JIRA_EMAIL",
  "token_env": "JIRA_API_TOKEN",
  "jql": "project = OPS ORDER BY updated ASC",
  "issue_limit": 50,
  "updated_lookback": "24h",
  "changelog_limit": 50,
  "remote_link_limit": 50
}
```

## Dependencies

- `internal/collector/jira` for source collection and envelope construction
- `internal/collector` for `ClaimedService`
- `internal/workflow` for collector instance parsing and claim state
- `internal/storage/postgres` for workflow and fact persistence
- `internal/runtime` and `internal/telemetry` for admin endpoints and signals

## Telemetry

The binary registers the shared runtime admin endpoints and Jira collector
signals:

- `/healthz`
- `/readyz`
- `/metrics`
- `/admin/status`
- `jira.observe`
- `jira.fetch`
- `eshu_dp_jira_provider_requests_total`
- `eshu_dp_jira_facts_emitted_total`
- `eshu_dp_jira_rate_limited_total`
- `eshu_dp_jira_fetch_duration_seconds`

## Gotchas / invariants

- `ESHU_JIRA_HEARTBEAT_INTERVAL` must stay lower than
  `ESHU_JIRA_CLAIM_LEASE_TTL`.
- Public examples and fixtures must not include real site IDs, issue keys, user
  identities, summaries, private URLs, or credential values.
- Jira remote links are source evidence only, webhooks are freshness triggers
  only, and polling remains the authoritative backfill path.

## Related docs

- [Jira Evidence Contract](../../../docs/public/reference/jira-evidence.md)
- `go/internal/collector/jira/README.md`
- `docs/public/reference/environment-collectors.md`
- `docs/public/deployment/service-runtimes-collectors.md`
