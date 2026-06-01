# collector-jira Agent Notes

This binary wires the Jira source package to workflow claims and Postgres.

- Do not add direct PagerDuty, GitHub, deployment, or graph dependencies here.
- Resolve credentials only from environment variable names in collector config.
- Keep `ESHU_JIRA_HEARTBEAT_INTERVAL < ESHU_JIRA_CLAIM_LEASE_TTL`.
- Add focused config tests before changing environment variable names or
  instance selection behavior.
