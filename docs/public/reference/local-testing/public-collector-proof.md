# No-Credential Public Collector Proof

This gate proves additional collector lanes live on the default local Compose
stack with **no operator credentials**, using only public, unauthenticated
endpoints. It complements the git-lane proof recorded in the
[All-Collector Readiness Proof Matrix](../collector-readiness-proof-matrix.md)
by bringing up the workflow-coordinator with a small, claim-driven collector
instance and asserting fact commit, reducer drain to zero, and API/MCP readback.

It is the agent-runnable answer to the coverage gap tracked by
[#3347](https://github.com/eshu-hq/eshu/issues/3347): beyond git, prove at least
the vulnerability-intelligence and package-registry lanes without secrets.

## What It Proves

| Lane | Public sources | Auth |
| --- | --- | --- |
| vulnerability intelligence | CISA KEV, FIRST EPSS, OSV | none |
| package registry | public npm (`registry.npmjs.org`) | none |

NVD is intentionally excluded because it is key-gated
(`ESHU_NVD_API_KEY`). The credential-backed lanes (ECR/JFrog OCI, AWS, GitHub
security alerts, PagerDuty, Jira, Grafana stack) stay in the operator-gated
[Remote Collector E2E](remote-collector-e2e.md) and
[Collector Live Smokes](collector-live-smokes.md) gates.

## Command

```bash
# Fast preflight: validates tooling and the claim-instance shape. No Docker,
# no network.
./scripts/verify_local_public_collector_proof.sh --check

# Live proof: builds the default stack, claim-drives the public collectors,
# waits for reducer drain, and asserts API + MCP readback.
./scripts/verify_local_public_collector_proof.sh
```

Required local dependencies: `docker` (Docker Compose v2 or `docker-compose`),
`curl`, `jq`, and `nc`. The live run reaches public network endpoints
(CISA KEV, FIRST EPSS, OSV, npm), so it cannot run fully green in an offline
sandbox; use `--check` there.

The script picks free host ports automatically, so it can run beside other local
stacks. Pass `--keep-stack` (or `ESHU_KEEP_COMPOSE_STACK=true`) to leave the
stack up for inspection.

## How It Works

1. Brings up the default `docker-compose.yaml` core (NornicDB, Postgres,
   db-migrate, API, MCP server, resolution-engine) plus the
   `workflow-coordinator` profile in `active` deployment mode with claims
   enabled.
2. Configures `ESHU_COLLECTOR_INSTANCES_JSON` with two bounded, public-only
   claim instances (KEV/EPSS/OSV and public npm). Page and version limits are
   kept small.
3. Claim-drives the `eshu-collector-vulnerability-intelligence` and
   `eshu-collector-package-registry` workers bound to those instance ids.
4. Waits for fact commit via anchorless API counts, then waits for the reducer
   queue to drain to zero with no `retrying`, `failed`, or `dead_letter` work.
5. Asserts API readback truth labels
   (`/api/v0/supply-chain/advisories`,
   `/api/v0/supply-chain/vulnerabilities/{id}`,
   `/api/v0/package-registry/packages/count`) and an MCP readback
   (`list_package_registry_packages` truth envelope).

## Public-Safe Output Contract

Output is aggregate-only: counts, states, and terminal queue depth. The script
never prints targets, tokens, account/tenant IDs, registry hosts, repository
names, raw provider locators, or machine-specific paths. It requires no operator
secrets. The deterministic unit test
`scripts/test-verify-local-public-collector-proof.sh` enforces that the claim
instances configure only the public sources above and never wire NVD
credentials.
