# Environment Variable Reference

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

This reference is generated from the code-owned registry in `go/internal/envregistry`. It covers the core platform subsystems. Run `eshu config validate` to check the current environment against it.

## api

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_API_ADDR` | string | `:8080` | API server listen address. |
| `ESHU_API_KEY` | string | — | Bearer token for API authentication. Setting it (or `ESHU_SCOPED_TOKENS_FILE`/`ESHU_AUTH_RESOURCE_URI`) also enables read enforcement: headerless requests to non-public routes are rejected with 401. With none of the three set the read surface is open (local/demo dev-mode). |
| `ESHU_API_SHUTDOWN_TIMEOUT` | duration | `30s` | Graceful shutdown deadline for the API HTTP server; an explicit 5 s setting is honored for backwards compatibility. |
| `ESHU_API_V0_SUNSET_DATE` | string | `Thu, 01 Jul 2027 00:00:00 GMT` | RFC 1123 GMT date after which /api/v0/ routes may be removed. Passed through as-is in the Sunset response header on every /api/v0/ response. |
| `ESHU_AUTH_COOKIE_SECURE` | enum | `auto` | Controls the Secure attribute on browser session and CSRF cookies (#4964). auto (default) keeps Secure set for every request except a plain-HTTP loopback origin (localhost, 127.0.0.1, ::1), which relaxes it so local development without TLS keeps a persistent session; any other plain-HTTP origin still gets Secure=true, so the browser silently drops the cookie there rather than the server ever issuing a non-Secure cookie outside loopback. always restores the pre-#4964 behavior: Secure is always set regardless of request origin. Allowed: `auto`, `always`. |
| `ESHU_AUTH_GITHUB_CONFIG_FILE` | string | — | Path to an operator-managed GitHub login config file (issue #5166, F-5). When set and not disabled, the API enables backend GitHub Authorization Code (plain OAuth2, non-discovery) login and reads provider/client/allowed-org/team-role mapping handles from this file. Every provider entry requires a non-empty allowed_orgs list — a GitHub OAuth App can authenticate any github.com account, so the org allow-list is the connector's only tenant boundary. |
| `ESHU_AUTH_GITHUB_ENABLED` | bool | `false` | Explicitly enables or disables backend GitHub login. Set true with no config file to run on DB-backed GitHub provider configs only (admin-registered via the provider CRUD API); set false to disable even when a config file is present. Two-part activation (issue #5605): a GitHub provider configured only through the admin API is not reachable at GET /api/v0/auth/github/login until this is also true at API startup and the API restarts — the DB row alone does not mount the route. |
| `ESHU_AUTH_GITHUB_PROVIDER_ID` | string | — | Optional default provider config id override for GitHub login. The id must reference a provider declared in ESHU_AUTH_GITHUB_CONFIG_FILE. |
| `ESHU_AUTH_GITHUB_SESSION_REFRESH_WINDOW` | duration | `15m` | Maximum staleness window for GitHub-backed browser sessions before the API revokes the session and requires fresh GitHub reauthentication. Explicit invalid or non-positive durations fail API startup closed. |
| `ESHU_AUTH_GITHUB_STATE_TTL` | duration | `10m` | GitHub OAuth2 login state lifetime (no nonce: plain OAuth2 has none). Explicit invalid durations fail API startup closed. |
| `ESHU_AUTH_OIDC_CONFIG_FILE` | string | — | Path to an operator-managed OIDC login config file. When set and not disabled, the API enables backend Authorization Code login and reads provider/client/group-role mapping handles from this file. role_grants[].policy_revision_hash is deprecated and ignored (#5038) - the live workspace policy revision hash is always resolved server-side instead. |
| `ESHU_AUTH_OIDC_ENABLED` | bool | `false` | Explicitly enables or disables backend OIDC login. Set true with ESHU_AUTH_OIDC_CONFIG_FILE to require OIDC startup config; set false to disable even when a config file is present. |
| `ESHU_AUTH_OIDC_LOGIN_PROVIDER_BURST` | int | `10` | Maximum burst size for the per-provider OIDC login rate limiter. |
| `ESHU_AUTH_OIDC_LOGIN_PROVIDER_RATE_PER_MIN` | int | `60` | Maximum OIDC login requests per minute per identity provider (by provider_config_id). Set to 0 to disable the per-provider limit. This is a coarse per-IdP defense — all login attempts to one provider share a single bucket. |
| `ESHU_AUTH_OIDC_LOGIN_RATE_BURST` | int | `20` | Maximum burst size for the per-IP OIDC login rate limiter. |
| `ESHU_AUTH_OIDC_LOGIN_RATE_PER_SEC` | int | `10` | Maximum OIDC login requests per second per client IP. Set to 0 to disable the per-IP limit. Requests exceeding this limit receive HTTP 429. |
| `ESHU_AUTH_OIDC_PROVIDER_ID` | string | — | Optional default provider config id override for OIDC login. The id must reference a provider declared in ESHU_AUTH_OIDC_CONFIG_FILE. |
| `ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE` | int | `200` | Maximum OIDC-backed browser sessions processed per bounded active-session revocation refresh pass. Keeps each pass proportional to the batch rather than the full session table. Non-positive values fail API startup closed. |
| `ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED` | bool | `false` | Enables the background OIDC active-session revocation refresh worker that re-resolves provider/user state for already-issued sessions within the staleness window and revokes sessions whose grants, role targets, or external subject are no longer valid. |
| `ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL` | duration | `1m` | Cadence of the bounded OIDC active-session revocation refresh worker. Non-positive durations fail API startup closed. |
| `ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW` | duration | `15m` | Maximum staleness window for OIDC-backed browser sessions before the API revokes the session and requires fresh IdP reauthentication. Explicit invalid or non-positive durations fail API startup closed. |
| `ESHU_AUTH_OIDC_STATE_TTL` | duration | `10m` | OIDC login state and nonce lifetime. Explicit invalid durations fail API startup closed. |
| `ESHU_AUTH_PREREGISTERED_CLIENT_ID` | string | — | Optional OAuth client_id a deployment has pre-registered with its authorization server, advertised in cmd/mcp-server's RFC 9728 protected-resource metadata document as the `eshu_preregistered_client_id` extension member (issue #5163, F-2). Informational only: an MCP client that cannot perform dynamic client registration (an Okta custom authorization server offers no anonymous DCR) copies this value into its own client-registration field. Requires ESHU_AUTH_RESOURCE_URI to be set for the discovery document to be served at all; omitted from the document when unset. |
| `ESHU_AUTH_RESOURCE_DOCUMENTATION` | string | — | Optional URL of human-readable documentation for the protected resource, advertised in cmd/mcp-server's RFC 9728 protected-resource metadata document as the OPTIONAL `resource_documentation` field (issue #5163, F-2). Requires ESHU_AUTH_RESOURCE_URI to be set; omitted from the document when unset. |
| `ESHU_AUTH_RESOURCE_URI` | string | — | Canonical Eshu API/MCP resource identifier (RFC 8707) that an IdP-issued bearer access token's aud claim must carry to be accepted by the internal/oidcbearer resolver (issue #5162). Shared, deployment-wide, single value across cmd/api and cmd/mcp-server; also the resource identifier F-2's RFC 9728 protected-resource metadata advertises. When unset, IdP bearer-token validation is disabled entirely (the resolver is never constructed) - Eshu's own scoped/shared token authentication is unaffected. Setting it also enables read enforcement (headerless requests to non-public routes are rejected with 401), the same as `ESHU_API_KEY`/`ESHU_SCOPED_TOKENS_FILE`. |
| `ESHU_AUTO_GENERATE_API_KEY` | bool | `false` | When true, auto-generate and persist an API key if none is set. |
| `ESHU_DISABLE_NEO4J` | bool | `false` | When true, disable the graph backend entirely. |
| `ESHU_HOME` | string | — | Root directory for persisted API key and configuration (defaults to ~/.eshu). |
| `ESHU_QUERY_PROFILE` | enum | `production` | Query execution profile. Allowed: `production`, `local_authoritative`, `local_lightweight`. |
| `ESHU_SCOPED_TOKENS_FILE` | string | — | Path to an operator-managed scoped-token registry file; API and MCP fail closed if the configured file is malformed or unreadable. Setting it also enables read enforcement (headerless requests to non-public routes are rejected with 401), the same as `ESHU_API_KEY`/`ESHU_AUTH_RESOURCE_URI`. |
| `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` | bool | `false` | When true, serve GET /api/v0/supply-chain/impact/findings from the maintained canonical winners read model (#3389) instead of read-time dedup (bounded O(page)). Honored by both the API and MCP server; enable only after the reducer maintainer has populated the winners table. Output is byte-identical. |

## auth

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_ADMIN_PASSWORD` | string | — | Bootstrap admin password, used with ESHU_ADMIN_USERNAME to seed the first local owner/admin identity from the environment. Superseded by ESHU_ADMIN_PASSWORD_FILE when both are set (issue #4963). |
| `ESHU_ADMIN_PASSWORD_FILE` | string | — | Path to a file holding the bootstrap admin password; takes precedence over ESHU_ADMIN_PASSWORD when both are set (issue #4963). |
| `ESHU_ADMIN_USERNAME` | string | — | Bootstrap admin login id. Set together with ESHU_ADMIN_PASSWORD or ESHU_ADMIN_PASSWORD_FILE to seed the first local owner/admin identity from the environment instead of generating one; the admin's MFA recovery code is still generated and printed once (issue #4963). |
| `ESHU_AUTH_BOOTSTRAP_MODE` | enum | `generated` | Local admin bootstrap seeding policy applied when no identities exist yet and ESHU_ADMIN_USERNAME/PASSWORD are unset. generated (default) requires a configured DEK (ESHU_AUTH_SECRET_ENC_KEY) and seals a crypto/rand-generated one-time admin credential for retrieval via `eshu admin initial-credential`; sso-only and disabled skip local admin seeding entirely (issue #4963). Allowed: `generated`, `sso-only`, `disabled`. |
| `ESHU_AUTH_SECRET_ENC_KEY` | string | — | Base64-encoded 32-byte primary data-encryption key (DEK) for sealing reversible identity secrets (one-time admin bootstrap credential, provider write-only secrets) with AES-256-GCM. Superseded by ESHU_AUTH_SECRET_ENC_KEY_FILE when both are set. Never auto-generated: an ephemeral DEK would make every previously sealed envelope permanently undecryptable after a restart (epic #4962). |
| `ESHU_AUTH_SECRET_ENC_KEY_FILE` | string | — | Path to a file holding the base64-encoded 32-byte primary DEK; takes precedence over ESHU_AUTH_SECRET_ENC_KEY when both are set (epic #4962). |
| `ESHU_AUTH_SECRET_ENC_KEY_ID` | string | — | Optional label for the primary DEK's key id, embedded in every envelope it seals for rotation bookkeeping. Defaults to the first 8 hex characters of SHA-256(key) when unset (epic #4962). |

## collector-aws-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_AWS_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_AWS_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_AWS_REDACTION_KEY` | string | — | Encryption key for redacting AWS secrets when sensitive service scans are enabled. |

## collector-azure-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_AZURE_COLLECTOR_CLAIM_LEASE_TTL` | duration | — | Workflow claim lease TTL used by claimed-live mode; defaults to the workflow default. |
| `ESHU_AZURE_COLLECTOR_HEARTBEAT_INTERVAL` | duration | — | Claim heartbeat interval for claimed-live mode; must be less than the claim lease TTL. |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this Azure collector instance. |
| `ESHU_AZURE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier for claimed-live mode; defaults to the hostname. |
| `ESHU_AZURE_FIXTURE_PAGES_JSON` | string | — | JSON fixture pages for single-lane offline Resource Graph or resourcechanges smoke testing; not used in production. |
| `ESHU_AZURE_POLL_INTERVAL` | duration | `5m` | Poll interval for discovering Azure targets. |
| `ESHU_AZURE_REDACTION_KEY_FILE` | string | — | Read-only file path for Azure redaction key material used to fingerprint tags, managed identities, and resource-change actors. |
| `ESHU_AZURE_TARGETS_JSON` | string | — | JSON array of Azure target scopes. Each target may set source_lane to resource_graph or fixture-only resource_changes. |

## collector-cicd-run

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-component-extension

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_COMPONENT_COLLECTOR_INSTANCE_ID` | string | — | Instance ID for the component-extension collector. |
| `ESHU_COMPONENT_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering component activations. |
| `ESHU_COMPONENT_COLLECTOR_SCOPE_KIND` | string | — | Scope kind for component activations (e.g. organization, team). |

## collector-gcp-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GCP_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GCP_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GCP_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_GCP_COLLECTOR_QUOTA_PROJECT_ID` | string | — | Optional billing/quota project id sent as x-goog-user-project on Cloud Asset Inventory requests. Leave unset for service-account/Workload Identity Federation ADC; set it when the resolved ADC is a user credential, which otherwise gets a 403 quota-project error. |

## collector-grafana

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GRAFANA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-jira

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_JIRA_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_JIRA_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_JIRA_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_JIRA_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_JIRA_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-kubernetes-live

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_KUBERNETES_LIVE_CLUSTERS_JSON` | string | — | JSON array of Kubernetes cluster targets with auth configuration. |
| `ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID for the kubernetes-live collector. |
| `ESHU_KUBERNETES_LIVE_POLL_INTERVAL` | duration | `5m` | Poll interval for discovering Kubernetes resources. |

## collector-loki

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_LOKI_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_LOKI_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_LOKI_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-oci-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_OCI_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_OCI_REGISTRY_TARGETS_JSON` | string | — | JSON array of OCI registry target configurations. |

## collector-package-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PACKAGE_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-pagerduty

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PAGERDUTY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PAGERDUTY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PAGERDUTY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PAGERDUTY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-prometheus-mimir

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-sbom-attestation

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_SBOM_ATTESTATION_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-security-alerts

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_SECURITY_ALERT_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-tempo

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_TEMPO_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_TEMPO_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_TEMPO_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## collector-terraform-state

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_TERRAFORM_SCHEMA_DIR` | string | — | Directory of Terraform provider schemas; defaults to the built-in schema directory. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT` | duration | `20s` | Legacy heartbeat interval alias. Deprecated; use `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL`. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_TFSTATE_REDACTION_KEY` | string | — | Encryption key for redacting Terraform state secrets. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | string | — | Versioned policy identifier for redacting sensitive keys. |
| `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` | string | — | Comma-separated leaf keys to redact; defaults to password,secret,token,access_key,private_key,certificate,key_pair. |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | int | `0` | Maximum bytes accepted per Terraform state source; 0 means unlimited. |

## collector-vault-live

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VAULT_LIVE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VAULT_LIVE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_VAULT_LIVE_REDACTION_KEY` | string | — | Encryption key for redacting sensitive Vault data. |

## collector-vulnerability-intelligence

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |

## component

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COMPONENT_ALLOW_IDS` | string | — | Comma-separated allowlist of component IDs. |
| `ESHU_COMPONENT_ALLOW_PUBLISHERS` | string | — | Comma-separated allowlist of component publishers. |
| `ESHU_COMPONENT_CORE_VERSION` | string | — | Required core version for component compatibility. |
| `ESHU_COMPONENT_HOME` | string | — | Root directory for the component/extension registry. |
| `ESHU_COMPONENT_REVOKE_IDS` | string | — | Comma-separated revoke list of component IDs. |
| `ESHU_COMPONENT_REVOKE_PUBLISHERS` | string | — | Comma-separated revoke list of component publishers. |
| `ESHU_COMPONENT_TRUST_MODE` | string | — | Component provenance verification mode. |

## coordinator

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | string | — | JSON array of desired collector instances reconciled by the coordinator. |
| `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted collectors. |
| `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted extensions. |
| `ESHU_WORKFLOW_COORDINATOR_AWS_FRESHNESS_CLAIM_LEASE_DURATION` | duration | `5m` | Claim lease duration for AWS freshness trigger handoff (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED` | bool | `false` | Enable claim-based workflow coordination. Aliases: `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS`. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL` | duration | — | TTL for workflow claim leases. |
| `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` | enum | `dark` | Workflow coordinator deployment mode. Allowed: `dark`, `active`. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT` | int | — | Reap batch limit for expired claims per pass. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY` | duration | — | Delay before requeuing expired claims. |
| `ESHU_WORKFLOW_COORDINATOR_FRESHNESS_CLAIM_REAP_LIMIT` | int | `100` | Reap batch limit for expired AWS/GCP freshness trigger claims per pass (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_GCP_FRESHNESS_CLAIM_LEASE_DURATION` | duration | `5m` | Claim lease duration for GCP freshness trigger handoff (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL` | duration | — | Heartbeat interval for claim owners. |
| `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` | duration | — | Expired-claim reaping interval. |
| `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` | duration | `30s` | Workflow state reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL` | duration | `30s` | Run-level reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON` | string | — | JSON tenant boundary configuration. |

## graph

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GRAPH_BACKEND` | enum | `nornicdb` | Graph database backend. Allowed: `neo4j`, `nornicdb`. |
| `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` | int | — | Per-class in-flight ceiling for canonical (repository/entity/structural-edge) graph writes; overrides ESHU_GRAPH_WRITE_MAX_IN_FLIGHT for this class only (issue #4448). Empty falls back to ESHU_GRAPH_WRITE_MAX_IN_FLIGHT. |
| `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` | int | — | Bounds concurrent in-flight graph writes per writer process so a bootstrap+reducer write storm cannot push the graph backend past its throughput knee and cascade into canonical-write timeouts (issue #4456 / #3624). A measured NornicDB concurrent-writer sweep showed write throughput peaks near 12-16 concurrent writers then collapses, with p99 latency climbing to the canonical-write timeout. Empty or non-positive disables the bound (legacy passthrough); the shipped Compose default is 8. Falls back for both per-class ceilings below when neither is set. |
| `ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT` | int | — | Per-class in-flight ceiling for semantic-entity graph writes; overrides ESHU_GRAPH_WRITE_MAX_IN_FLIGHT for this class only (issue #4448). Empty falls back to ESHU_GRAPH_WRITE_MAX_IN_FLIGHT. |
| `ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT` | duration | `1m` | Timeout for acquiring a graph connection from the pool. |
| `ESHU_NEO4J_DATABASE` | string | — | Graph backend database name; defaults to neo4j for neo4j and nornic for nornicdb. Aliases: `NEO4J_DATABASE`. |
| `ESHU_NEO4J_MAX_CONNECTION_LIFETIME` | duration | `1h` | Graph connection lifetime before pool recycling. |
| `ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE` | int | `100` | Maximum graph driver connection pool size. |
| `ESHU_NEO4J_PASSWORD` | string | — | Graph backend password (falls back to NEO4J_PASSWORD). Aliases: `NEO4J_PASSWORD`. |
| `ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT` | duration | `5s` | Graph backend TCP socket connect timeout. |
| `ESHU_NEO4J_URI` | string | — | Graph backend Bolt URI (falls back to NEO4J_URI). Aliases: `NEO4J_URI`. |
| `ESHU_NEO4J_USERNAME` | string | — | Graph backend username (falls back to NEO4J_USERNAME). Aliases: `NEO4J_USERNAME`. |
| `ESHU_NEO4J_VERIFY_TIMEOUT` | duration | `10s` | Timeout for graph driver connectivity verification. |

## mcp

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_MCP_ADDR` | string | `:8080` | MCP HTTP transport listen address. |
| `ESHU_MCP_ALLOW_UNAUTHENTICATED` | bool | `false` | Dev/loopback escape hatch (issue #5168): when true, ESHU_MCP_TRANSPORT=http is allowed to start with no resolvable credential source (no ESHU_API_KEY, no ESHU_SCOPED_TOKENS_FILE, no ESHU_AUTH_RESOURCE_URI). Without it, that configuration refuses to start. Every initialize/tools/list/tools/call/ping request and SSE session is unauthenticated in that state; never set this on a publicly reachable port. |
| `ESHU_MCP_TOKEN` | string | — | Per-user Eshu API/MCP bearer token referenced -- never inlined -- by `eshu mcp setup` client snippets in token posture (issue #5169, F-8). Create it in the console (Profile -> API tokens, issue #5164) or via POST /api/v0/auth/local/api-tokens. The MCP client process interpolates it into the Authorization header; Eshu server processes never read this variable. `eshu mcp setup --verify` uses it for the staged probes when --api-key is not set. |
| `ESHU_MCP_TRANSPORT` | enum | `http` | MCP server transport mode. Allowed: `http`, `stdio`. |

## postgres

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_DEFER_CONTENT_SEARCH_INDEXES` | bool | `false` | Cold-bootstrap schema mode. When true, schema bootstrap creates the content tables without the two exact substring trigram GIN indexes; bootstrap-index restores and validates them after source-local content projection drains. Existing indexes are never dropped. Keep false unless bootstrap-index is guaranteed to run to successful finalization. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | duration | `10m` | Idle timeout before a connection is closed. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | duration | `30m` | Connection lifetime before recycling. |
| `ESHU_POSTGRES_DSN` | dsn | — | Postgres connection string. DSN precedence is ESHU_FACT_STORE_DSN, then ESHU_CONTENT_STORE_DSN, then ESHU_POSTGRES_DSN. Aliases: `ESHU_FACT_STORE_DSN`, `ESHU_CONTENT_STORE_DSN`. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | int | `10` | Maximum idle Postgres connections (capped at max open). |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | int | `30` | Maximum open Postgres connections. |
| `ESHU_POSTGRES_PING_TIMEOUT` | duration | `10s` | Timeout for the startup/readiness connectivity ping. |

## projector

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for projector work items. |
| `ESHU_PROJECTOR_RETRY_DELAY` | duration | `30s` | Delay between projector work-item retries. |
| `ESHU_PROJECTOR_WORKERS` | int | — | Concurrent projector workers (default NumCPU capped at 8, min 1). |

## reducer

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT` | int | `500` | Maximum stale value-flow evidence nodes or edges deleted per active scope and family in one reducer cleanup pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED` | bool | `true` | Enable the reducer side runner that removes stale CodeTaintEvidence nodes and TAINT_FLOWS_TO edges from older active-scope generations. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER` | string | — | Lease owner for the single value-flow stale cleanup worker; defaults to a unique process token. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL` | duration | `5m` | TTL for the value-flow stale cleanup partition lease. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL` | duration | `1h` | Delay between value-flow stale cleanup passes. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT` | int | `100` | Active repository scopes scanned per value-flow stale cleanup pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT` | int | `100` | Maximum graph orphan nodes deleted per label in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` | int | `10000` | Maximum graph orphan nodes counted per label for telemetry in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED` | bool | `true` | Enable the reducer side runner that marks and sweeps stale generation-owned graph orphans. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER` | string | — | Lease owner for the single graph orphan sweep worker; defaults to a unique process token. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL` | duration | `5m` | TTL for the graph orphan sweep partition lease. |
| `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL` | duration | `1h` | Delay between graph orphan sweep passes. |
| `ESHU_GRAPH_ORPHAN_SWEEP_TTL` | duration | `168h` | Minimum age before a marked graph orphan can be deleted. |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | int | `10000` | Ingester source-local reducer-intent admission threshold; defers while outstanding reducer queue depth is at or above this value. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | duration | `1s` | Queue-depth recheck interval while reducer admission is deferring. |
| `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` | int | `500` | Ingester graph-write backpressure: defers source-local reducer-intent admission while retrying-state reducer depth (the durable signal of recurring graph-write timeouts) is at or above this value, so recoverable work is throttled instead of dead-lettered. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` | int | `100` | Hysteresis floor for graph-write backpressure; admission resumes only after retrying-state reducer depth falls below this value. Must be less than ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | int | — | Work items claimed per cycle (default adaptive to workers and backend). |
| `ESHU_REDUCER_CLAIM_DOMAIN` | string | — | Single reducer claim domain. Deprecated; use `ESHU_REDUCER_CLAIM_DOMAINS`. |
| `ESHU_REDUCER_CLAIM_DOMAINS` | string | — | Comma-separated reducer domains for multi-domain claims. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for reducer work items. |
| `ESHU_REDUCER_RETRY_DELAY` | duration | `30s` | Delay between reducer work-item retries. |
| `ESHU_REDUCER_WORKERS` | int | — | Concurrent reducer workers (default derived from CPU count and backend). |

## runtime

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_LISTEN_ADDR` | string | `0.0.0.0:8080` | Primary HTTP listen address (host:port). |
| `ESHU_METRICS_ADDR` | string | `0.0.0.0:9464` | Prometheus metrics listen address (host:port). |
| `ESHU_PPROF_ADDR` | string | — | Opt-in pprof profiler address; unset disables it; a port-only value binds to 127.0.0.1. |

## semantic

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | string | — | JSON semantic extraction policy controlling source/provider ACL decisions. |
| `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` | bool | `false` | Default-off flag permitting real provider traffic (requires security review). |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | string | — | JSON array of semantic provider profile configurations, including optional search embedding dimensions. |
| `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` | bool | `false` | Enable the semantic-provider worker claim loop. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | enum | — | Deterministic no-network or auto-local semantic search selector for API, MCP, and reducer. Allowed: `hash`, `local_hash`, `auto_hash`. |
| `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` | string | — | Selects one governed search_documents provider profile when multiple semantic search providers are configured. |
