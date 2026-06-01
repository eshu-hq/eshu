# collector-jira

`collector-jira` is the hosted Jira work-item evidence collector. It selects an
enabled, claim-capable `jira` collector instance from
`ESHU_COLLECTOR_INSTANCES_JSON`, claims Jira site work, fetches bounded updated
windows from Jira Cloud, and commits `work_item.*` source facts.

## Required Environment

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

## Target Shape

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

The collector emits source facts only. Incident, deployment, code, pull-request,
and work-item correlations are reducer/query responsibilities.
