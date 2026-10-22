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
| `ESHU_CONTENT_SEARCH_BUDGET_MS` | int | `800` | SQL work budget in milliseconds for one content search with no repository filter: file-content search (POST /api/v0/content/files/search and the MCP search_file_content tool) and entity-content search (POST /api/v0/content/entities/search and the MCP search_entity_content tool, #7730). Inside it the search runs a key-ordered probe, capped continuation steps, a bounded trigram tail, then the rest of the budget as continuation (file search: 200-row probe, 500-row steps; entity search: 1,000-row probe, 5,000-row steps, #7009); when the budget ends first the answer is HTTP 200 with truncated true, truth level partial, and a data.partial object (reason, rows_scanned_in_order, rows_matched, cursor, budget_ms, elapsed_ms, overrun_ms, progressed, hint) instead of an unbounded scan. Every internal timeout scales linearly with it. Accepts 100..10000; any other value fails API and MCP server startup. A cancelled statement can run past its timeout by one candidate's recheck; data.partial.overrun_ms reports it, so the call can exceed the budget by that amount. Read once at startup. |
| `ESHU_DISABLE_NEO4J` | bool | `false` | When true, disable the graph backend entirely. |
| `ESHU_GOVERNANCE_MODE` | enum | `local_no_policy` | Hosted governance posture for the deployment. Both cmd/api and cmd/mcp-server read it: it is echoed in the governance status readback, and it decides whether a tenant-bound all-scope credential may use routes whose handlers filter reads by repository or scope grant. hosted_multi_tenant -- and any unrecognized value -- refuses such a caller with 403 permission_denied and records scoped_route_all_scope_grant_required; local_no_policy, hosted_single_tenant, and an unset mode admit it, the intended posture where one graph belongs to one tenant. Credentials carrying real repository or scope ids are unaffected in every mode. Allowed: `local_no_policy`, `hosted_single_tenant`, `hosted_multi_tenant`. |
| `ESHU_HOME` | string | — | Root directory for persisted API key and configuration (defaults to ~/.eshu). |
| `ESHU_INFRA_SEARCH_KEY_READ` | enum | `off` | When on, POST /api/v0/infra/resources/search serves free-text queries on Neo4j from the infra_search_key_ft FULLTEXT index (#7245) instead of scanning every infra label. Honored by both the API and MCP server; any other value fails startup. The index read runs only while a readiness probe (at start, then every 5 minutes) reads the index ONLINE and finds no unkeyed infra node that holds a searched field, and only for queries of at most 256 code points; every other request, NornicDB, and structured searches keep the legacy statement. Turn it on only after the bootstrap backfill has keyed the store and eshu_dp_infra_search_key_ready reads 1. Allowed: `off`, `on`. |
| `ESHU_QUERY_PROFILE` | enum | `production` | Query execution profile. Allowed: `production`, `local_authoritative`, `local_lightweight`. |
| `ESHU_SCOPED_TOKENS_FILE` | string | — | Path to an operator-managed scoped-token registry file; API and MCP fail closed if the configured file is malformed or unreadable. Setting it also enables read enforcement (headerless requests to non-public routes are rejected with 401), the same as `ESHU_API_KEY`/`ESHU_AUTH_RESOURCE_URI`. |
| `ESHU_STATUS_DIAGNOSTIC_REPORT` | string | — | Optional private output path for bounded status GET request diagnostics. Unset disables capture; selected responses carry server-generated IDs. At graceful shutdown a sanitized report marks incomplete or overflowing evidence invalid. Does not change authentication, query or work/latency budgets. |
| `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` | bool | `false` | When true, serve GET /api/v0/supply-chain/impact/findings from the maintained canonical winners read model (#3389) instead of read-time dedup (bounded O(page)). Honored by both the API and MCP server; enable only after the reducer maintainer has populated the winners table. Output is byte-identical. |

## auth

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_ADMIN_PASSWORD` | string | — | Bootstrap admin password, used with ESHU_ADMIN_USERNAME to seed the first local owner/admin identity from the environment. Superseded by ESHU_ADMIN_PASSWORD_FILE when both are set (issue #4963). |
| `ESHU_ADMIN_PASSWORD_FILE` | string | — | Path to a file holding the bootstrap admin password; takes precedence over ESHU_ADMIN_PASSWORD when both are set (issue #4963). |
| `ESHU_ADMIN_USERNAME` | string | — | Bootstrap admin login id. Set together with ESHU_ADMIN_PASSWORD or ESHU_ADMIN_PASSWORD_FILE to seed the first local owner/admin identity from the environment instead of generating one; the admin's MFA recovery code is still generated and printed once (issue #4963). |
| `ESHU_AUTH_BOOTSTRAP_MODE` | enum | `generated` | Local admin bootstrap seeding policy applied when no identities exist yet and ESHU_ADMIN_USERNAME/PASSWORD are unset. generated (default) requires a configured DEK (ESHU_AUTH_SECRET_ENC_KEY) and seals a crypto/rand-generated one-time admin credential for retrieval via `eshu admin initial-credential`; sso-only and disabled skip local admin seeding entirely (issue #4963). Allowed: `generated`, `sso-only`, `disabled`. |
| `ESHU_AUTH_SECRET_ENC_KEY` | string | — | Base64-encoded 32-byte primary data-encryption key (DEK) for sealing reversible identity secrets (one-time admin bootstrap credential, provider write-only secrets) with AES-256-GCM. Also seals the `GET /api/v0/images/tag-history` continuation cursor (#6564), which makes this a key the **mcp-server** needs too, not only the api: without it a grant-filtered caller's tag-history paging fails closed on that process (`next_cursor` omitted, 503 for a request carrying a cursor) while unscoped paging is unaffected. Superseded by ESHU_AUTH_SECRET_ENC_KEY_FILE when both are set. Never auto-generated: an ephemeral DEK would make every previously sealed envelope permanently undecryptable after a restart (epic #4962). |
| `ESHU_AUTH_SECRET_ENC_KEY_FILE` | string | — | Path to a file holding the base64-encoded 32-byte primary DEK; takes precedence over ESHU_AUTH_SECRET_ENC_KEY when both are set (epic #4962). |
| `ESHU_AUTH_SECRET_ENC_KEY_ID` | string | — | Optional label for the primary DEK's key id, embedded in every envelope it seals for rotation bookkeeping. Defaults to the first 8 hex characters of SHA-256(key) when unset (epic #4962). |

## backend-conformance

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_LIVE` | string | — | Set to 1 to run the opt-in CloudResource owner-ledger backfill scale proof (go/internal/query) against the disposable NornicDB named by ESHU_NEO4J_URI; it seeds 150k nodes and skips when unset. |
| `ESHU_DIFFERENTIAL_CAPTURE` | string | — | Set to 1 to record differential statement fingerprints during a gate run; recording additionally requires ESHU_DIFFERENTIAL_CAPTURE_DIR, and a stray flag alone never records outside a gate run. |
| `ESHU_DIFFERENTIAL_CAPTURE_DIR` | string | — | Directory differential recording files land in; the run fails closed when ESHU_DIFFERENTIAL_CAPTURE is set without it. |
| `ESHU_MODULE_IMPORT_KEY_MIGRATION_LIVE` | string | — | Set to 1 to run the #8168 Module import_key migration live test (go/cmd/bootstrap-data-plane) against a disposable Neo4j named by the live-test Bolt DSN; the migration rewrites every canonical Module in that database, so never point it at a shared backend. The live-backend CI runner enables the test on its fresh per-file Neo4j backend without this variable. |
| `ESHU_PRIVATE_IDENTIFIERS_FILE` | string | — | Path to a file outside git listing organisation and product identifiers, one per line, that the cassette private-data gate (scripts/verify-cassette-author.sh) must not find in testdata/cassettes; unset scans for the committed canary only and says so on stderr (#6965). In CI, same-repo PRs load this file from the private-identifiers repository secret (#6979). No-Regression Evidence: description-string change only; no runtime, queue, or graph path touched. No-Observability-Change: no new spans, metrics, or logs. |
| `ESHU_PRIVATE_IDENTIFIERS_REQUIRED` | string | — | Set to 1 to make the cassette private-data gate fail when ESHU_PRIVATE_IDENTIFIERS_FILE is unset, instead of running the identifier check with the canary only (#6965). CI sets this for the cassette-author gate on same-repo PRs (#6979). |
| `ESHU_RECORD_PSEUDONYM_KEY` | string | — | Recording key for collector -mode=record pseudonymization (#6965 Phase 3): at least 32 bytes, generated with `openssl rand -hex 32`, one key per cassette corpus so cross-cassette joins survive, kept with the corpus's other secrets and never committed; only its 8-hex fingerprint is written to the cassette and logged. |
| `ESHU_REPOS_DIR` | string | — | Repository checkout directory override shared by collectors, CLI supervision, and gate scripts; each reader falls back to its own default when unset. |

## collector

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GITLAB_GROUP` | string | — | GitLab group full path (acme/platform, subgroups included) or numeric ID for the gitlabGroup repository source mode (#7765). Required when ESHU_REPO_SOURCE_MODE=gitlabGroup; ignored by every other mode. An empty value fails collector startup in gitlabGroup mode. |
| `ESHU_GITLAB_HOST` | string | `gitlab.com` | GitLab provider host for gitlab/ repository IDs (#7764). Unset keeps gitlab.com. Set it to a self-managed bare hostname (no scheme, port, userinfo, or path) so explicit-mode clone, fetch, SSH, and token-auth URLs use that host; an invalid value fails collector startup. |
| `ESHU_GIT_CLONE_TIMEOUT_SECONDS` | int | `600` | Per-command deadline in seconds for the initial git clone of one repository (#8052). It is the longest deadline because a large clone that is still making progress is not a stall, and a short deadline would make that repository fail on every cycle. Same behavior at the deadline (one attempt of the bounded retry, ESHU_GIT_RETRY_ATTEMPTS) and same validation as ESHU_GIT_RESOLVE_TIMEOUT_SECONDS. Unset keeps 600. |
| `ESHU_GIT_FETCH_TIMEOUT_SECONDS` | int | `90` | Per-command deadline in seconds for each git fetch the git collector's repository sync runs into an existing checkout, including pinned-ref fetches and the shallow.lock retry (#8052). At the deadline git and its remote-https helper are killed and the kill is counted in eshu_dp_git_command_deadline_exceeded_total{operation=fetch}, once per killed attempt. A kill is one attempt of the bounded retry (ESHU_GIT_RETRY_ATTEMPTS), logged as git command attempt failed. After the last attempt a default-branch fetch is logged as one git repository sync failed with failure_class=deadline_exceeded, counted in eshu_dp_git_repo_sync_failures_total, and the repository is retried next cycle. A pinned-ref fetch that hits the deadline is counted in eshu_dp_git_command_deadline_exceeded_total{operation=fetch} (two fetches per pinned ref, branch then tag) and logged in the failed to fetch pinned ref; skipping warning, which then carries failure_class=deadline_exceeded, deadline_operation, deadline_seconds, operation and elapsed_seconds (it is not a git repository sync failed line and is not in eshu_dp_git_repo_sync_failures_total); that repository's other refs and the cycle continue. Same validation as ESHU_GIT_RESOLVE_TIMEOUT_SECONDS. Unset keeps 90. Purely local git commands (checkout, rev-parse, status, worktree) have no deadline. Fetch and clone also pass http.lowSpeedLimit=1 and http.lowSpeedTime=60, so a transfer stuck at 0 bytes per second for 60 s aborts on its own. |
| `ESHU_GIT_RESOLVE_CACHE` | enum | `on` | Kill switch for the git collector's per-host address cache (#8092). When on, the collector resolves each git provider host with Go's resolver and passes every A and AAAA address to clone, fetch, and ls-remote as -c http.curloptResolve, so libcurl skips its own lookup in each new git process; certificate and host name checks are unchanged. A refresh is requested before a retry whose failure is a deadline kill or one of the closed curl wording rows (could not resolve host, resolving timed out, failed to connect, connection timed out, connection reset, Operation timed out after), never for an HTTP status, auth, or pack answer. Requests are single-flight per host with a 30 s minimum gap; a failed refresh keeps the last good set and an empty set is never written. When off, git resolves as before the cache existed. Nothing is pinned when HTTP_PROXY, HTTPS_PROXY, or ALL_PROXY (either case) is set, and SSH remotes are unaffected. Counted in eshu_dp_git_resolve_cache_lookups_total, eshu_dp_git_resolve_cache_refresh_requests_total, eshu_dp_git_resolve_cache_fallback_total, and eshu_dp_git_resolve_cache_lookup_duration_seconds. Any value other than on or off stops startup with an error that names the variable and the value. Allowed: `on`, `off`. |
| `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS` | int | `5` | Deadline in seconds for one Go-resolver lookup of a git provider host by the address cache (#8092), and the longest the first network git command for a host waits for its curloptResolve pin before running without it, counted as eshu_dp_git_resolve_cache_fallback_total{reason=cache_empty}. Unset keeps 5; a set value must be a whole number of seconds from 1 to 60, and anything else stops startup with an error that names the variable and the value. |
| `ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS` | int | `3600` | Background refresh period in seconds of the git address cache (#8092): each known provider host is re-resolved once per period through the same single-flight path as a failure refresh, replacing its addresses only on success and never evicting, so a provider address rotation costs at most one command deadline instead of one stall per fetch. Counted as eshu_dp_git_resolve_cache_refresh_requests_total{reason=periodic}. Unset keeps 3600; a set value must be a whole number of seconds from 60 to 86400, and anything else stops startup with an error that names the variable and the value. |
| `ESHU_GIT_RESOLVE_TIMEOUT_SECONDS` | int | `60` | Per-command deadline in seconds for the git collector's remote ref discovery: git ls-remote for the ref list and for the remote default-branch lookup (#8052). The sync loop is sequential, so a command stuck on name resolution otherwise pauses the whole fleet sync for libcurl's 300 s. At the deadline the collector kills git and its remote-https helper and counts eshu_dp_git_command_deadline_exceeded_total; the kill is one attempt of the bounded retry (ESHU_GIT_RETRY_ATTEMPTS), logged as git command attempt failed. After the last attempt it logs one git repository sync failed with failure_class=deadline_exceeded and the repository is retried next cycle. Unset keeps 60; a set value must be a whole number of seconds from 1 to 86400, and anything else (negative, zero, fractional, non-numeric) stops startup with an error that names the variable and the value. |
| `ESHU_GIT_RETRY_ATTEMPTS` | int | `2` | How many times the git collector runs one network git command (clone, fetch, ls-remote) before its failure is final (#8091): the first attempt and, by default, one retry. Only a connectivity failure is retried: a deadline kill, a name-resolution or connect failure, a connection reset or a transfer closed mid-way, an HTTP/2 framing or stream error, a TLS handshake error, a low-speed abort, and git's remote end hung up unexpectedly when it comes with a transport error line. HTTP 4xx, authentication, object integrity errors, repository not found, rate limits, pack errors that are not preceded by a transport error line, and any text the classifier does not name are never retried. Each attempt gets its own full deadline and a jittered 3 s backoff, so one command adds at most attempts times its operation deadline plus the backoffs, and a repository runs several commands per sweep (ref listing, a fetch, and on a default-branch change the branch lookup and a second fetch); a fetch that hits a stale shallow lock runs the command a second time through the existing lock recovery, so one fetch can make up to 2 x attempts attempts. Before a fetch retry the collector removes a shallow.lock that the killed attempt left. Every failed attempt logs git command attempt failed with failure_class, will_retry and backoff_seconds, a recovery logs git command recovered on attempt N, and eshu_dp_git_command_attempts_total, eshu_dp_git_command_retries_total and eshu_dp_git_command_attempt_duration_seconds count them. The final failure is still one git repository sync failed line. Set 1 to turn the retry off. Unset keeps 2; a set value must be a whole number from 1 to 5, and anything else stops startup with an error that names the variable and the value. |
| `ESHU_GIT_RETRY_SWEEP_BUDGET` | int | `20` | How many retries one sync sweep may start across all repositories (#8091). It is the global cap behind ESHU_GIT_RETRY_ATTEMPTS: a provider outage that fails every repository spends this budget once and every later failure is final on its first attempt, exactly as before the retry existed, so the sweep is not multiplied by the attempt count. The refusal is logged with retry_budget_exhausted=true and counted in eshu_dp_git_command_retry_budget_exhausted_total{scope=sweep}. Unset keeps 20; a set value must be a whole number from 1 to 1000, and anything else stops startup with an error that names the variable and the value. |
| `ESHU_REPO_RECONCILE_INTERVAL_HOURS` | int | `24` | Hours a git scope may go without a full reconciliation sweep before the ingester's repo-selection collector forces one to retract drift the incremental delta path missed (epic #2340). An explicit 0 disables reconciliation; unset or invalid falls back to the default. Clamped to whole hours. |
| `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` | int | `10` | Maximum git scopes forced to a full reconciliation snapshot in one repo-selection cycle (epic #2340). An explicit 0 removes the per-cycle cap while reconciliation stays interval-gated; unset or invalid falls back to the default. |
| `ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS` | int | `7` | Skipped-parse valve for the reconciliation sweep (#7978). A forced reconcile of an unchanged repository whose pre-parse identity equals its active generation's re-projects that generation without parsing; this setting makes such a repository parse at least every N intervals (N-1 skips after each parse), so an input the identity does not cover stays stale at most N intervals. Unset keeps the default; 0 never skips the parse (the behaviour before the skip); a set value must be a non-negative integer, and a negative or non-integer value stops startup with an error that names the variable and the value (ruling a9-143.1). A skipped parse still spends a ESHU_REPO_RECONCILE_MAX_PER_CYCLE slot. |
| `ESHU_REPO_RECONCILE_STAMP_IDENTITY_WITHOUT_PARSE` | bool | `false` | Opt-in first-sweep identity stamp for the reconciliation sweep (#8095). When true, a forced reconcile of an unchanged repository with no stored pre-parse identity stamps the identity without a full parse, but only when the active generation proves it was produced by the running content version and configuration with no degraded snapshot; otherwise it parses as before. Off by default, so an unset value keeps the first sweep parsing once to stamp one. A stamp counts as a parse for ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS and still spends a ESHU_REPO_RECONCILE_MAX_PER_CYCLE slot. |
| `ESHU_REPO_SCHEDULED_SYNC_ENABLED` | bool | `true` | Enables the git ingester's scheduled repository selection, the only selection that visits every repository and therefore the only one that honors every reindex request (#7620, #7797). Accepts true/false, 1/0, yes/no, on/off; any other value fails startup. false requires ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED=true. |
| `ESHU_REPO_SELECTION_LIVENESS_WINDOW` | duration | `48h` | How long a git collector's repository selection observation still counts after its evaluation (#7625). The collector stores it on every row; a scope whose rows are all older than their window reads selection unknown instead of not_selected. Minimum 1h; smaller or unparsable values fail config load. Keep it above the longest gap between selection cycles. |
| `ESHU_REPO_SOURCE_MODE` | enum | `githubOrg` | Git collector repository source mode. githubOrg lists one GitHub organization, explicit syncs the configured repository list, filesystem scans a local root, and gitlabGroup lists one GitLab group including subgroups (#7765, token auth only). Allowed: `githubOrg`, `explicit`, `filesystem`, `gitlabGroup`. |
| `ESHU_WEBHOOK_TRIGGER_CLAIM_LEASE_WINDOW` | duration | `15m` | Stale-claim lease for webhook refresh triggers: a claim older than this without a handoff goes back to queued, or to failed past the attempt cap (#7661). Empty or invalid keeps the default. |
| `ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED` | bool | `false` | Makes the git ingester claim queued webhook refresh triggers before scheduled selection. A webhook-only ingester (scheduled sync off) syncs only triggered repositories, so it honors a reindex request only for a repository a webhook triggers. 1, true, yes, and on enable it; any other value disables it. |
| `ESHU_WEBHOOK_TRIGGER_MAX_CLAIM_ATTEMPTS` | int | `3` | Lifetime claim attempts per webhook refresh trigger before a reap fails it with claim_lease_exhausted (#7661). Empty or below 1 keeps the default. |
| `ESHU_WEBHOOK_TRIGGER_SWEEP_MAX_YIELDS` | int | `8` | How many times one scheduled sweep may stop at a repository boundary for a queued accepted webhook trigger before it must run to completion (#8123), so a steady trigger stream cannot starve sweep coverage. The waited time shows in eshu_dp_webhook_trigger_claim_wait_seconds and each yield in eshu_dp_collector_sweep_yields_total. Empty or below 1 keeps the default. |
| `ESHU_WEBHOOK_TRIGGER_SWEEP_YIELD_CHECK_REPOS` | int | `16` | Repositories a scheduled sweep visits between queued-trigger probes (#8123): a trigger arriving mid-sweep is noticed at the first probe past its arrival and claimed at the next collector cycle, after the partial batch drains. Smaller values claim sooner and probe Postgres more often. Empty or below 1 keeps the default. |

## collector-aws-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_AWS_ALLOW_EMPTY_SUCCESSOR` | bool | `false` | Collector-wide trusted-deletion attestation: when true, every claimed AWS generation carries TrustedDeletion, bypassing the ingestion store's successor-emptiness backstop for legitimately emptied estates. Prefer the per-scope allow_empty_successor key when only some scopes may legitimately empty. |
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
| `ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR` | bool | `false` | Collector-wide trusted-deletion attestation: when true, every OCI registry generation carries TrustedDeletion, bypassing the ingestion store's successor-emptiness backstop for legitimately emptied repositories. Prefer the per-target allow_empty_successor key when only some repositories may legitimately empty. |
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
| `ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR` | bool | `false` | Collector-wide trusted-deletion attestation: when true, a parsed state with the legitimate-empty shape (snapshot only, zero body facts) carries TrustedDeletion, bypassing the ingestion store's successor-emptiness backstop for legitimately emptied states. Missing, over-ceiling, and identity-mismatched states never attest. Prefer the allow_empty_successor runtime configuration key when the environment is managed per instance. |
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
| `ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION` | int | — | Uniform operator override for per-component facts-per-generation quota (#8561): declared and validated today, not runtime-enforced; blank keeps the manifests. |
| `ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR` | int | — | Uniform operator override for per-component generations-per-hour claim quota (#8561): wins over every admitted-manifest declaration of a component planning at least one claims-enabled instance (#8582); blank keeps the manifests. |
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
| `ESHU_CANONICAL_RETRACT_BATCH` | int | `2000` | Nodes deleted per iteration of the NornicDB bounded full-refresh retract drain in the ingester and projector (1-10000; with the nornicdb graph backend, an invalid or out-of-range value fails startup). Size it against the per-iteration canonical write timeout. |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | duration | — | Transaction timeout for canonical and reducer graph writes (Go duration). On NornicDB an unset value uses 30s. On Neo4j an unset or invalid value uses the 300s default (issue #7471); only an explicit non-positive duration leaves Neo4j writes unbounded, and each writer logs graph.write_timeout.unbounded at startup in that case (issue #7046). Keep lease TTLs that guard graph writes above this value. |
| `ESHU_GRAPH_BACKEND` | enum | `nornicdb` | Graph database backend. Allowed: `neo4j`, `nornicdb`. |
| `ESHU_GRAPH_READ_SLOW_THRESHOLD` | duration | `1s` | Duration a bounded Neo4jReader graph read must reach or exceed before it is logged and counted as a slow outcome (issue #7035). Defaults to 1 second (it was 2 seconds before #7035). An invalid or non-positive value logs once and falls back to the default; it never disables the warning. |
| `ESHU_GRAPH_SCHEMA_ADOPT_ONLY` | bool | `false` | Opt-in fail-closed mode for eshu-bootstrap-data-plane. Accepts Go boolean spellings (1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False) with optional surrounding whitespace; unset is false. Whitespace alone, yes, on, and TrUe are invalid before stores open. A missing or incompatible graph marker must adopt a complete inspected catalog; a mismatch, inspection error, or absent inspector refuses graph DDL and a new marker. Forced reapply and disabled adoption are rejected before stores open. Postgres migrations still run before graph inspection. Catalog names alone do not prove object definitions or index ONLINE state. |
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
| `ESHU_NORNICDB_PROFILE_FILE_GROUPS` | bool | `false` | Ingester-only opt-in timing logs for canonical NornicDB File-group statement attempts. Emits fixed template IDs, row counts, and Run, Consume, and post-callback durations without query text, parameters, file paths, or raw errors. Disable after a bounded diagnostic capture. Cannot be enabled alongside the older grouped-write profiler. |

## mcp

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_MCP_ADDR` | string | `:8080` | MCP HTTP transport listen address. |
| `ESHU_MCP_ALLOW_UNAUTHENTICATED` | bool | `false` | Dev/loopback escape hatch (issue #5168): when true, ESHU_MCP_TRANSPORT=http is allowed to start with no resolvable credential source (no ESHU_API_KEY, no ESHU_SCOPED_TOKENS_FILE, no ESHU_AUTH_RESOURCE_URI). Without it, that configuration refuses to start. Every initialize/tools/list/tools/call/ping request and SSE session is unauthenticated in that state; never set this on a publicly reachable port. |
| `ESHU_MCP_TOKEN` | string | — | Per-user Eshu API/MCP bearer token referenced -- never inlined -- by `eshu mcp setup` client snippets in token posture (issue #5169, F-8). Create it in the console (Profile -> API tokens, issue #5164) or via POST /api/v0/auth/local/api-tokens. The MCP client process interpolates it into the Authorization header; Eshu server processes never read this variable. `eshu mcp setup --verify` uses it for the staged probes when --api-key is not set. |
| `ESHU_MCP_TOOL_SURFACE` | enum | `full` | Which tools MCP tools/list advertises (issue #8758, epic #8752): full lists every registered tool (today's list, unchanged), router lists find_tool and call_tool only, core_router lists a small core set plus find_tool and call_tool. It changes advertisement only: a tool the surface hides still dispatches on tools/call, tools hidden by ESHU_QUERY_PROFILE stay hidden from tools/list in every mode, and the profile's per-call gate is unchanged. An unknown value refuses to start. A client can override it with the surface query parameter: on POST /mcp/message it applies to that request, and on GET /sse it applies to the whole SSE session (the endpoint event carries it, and a POST that names its own surface still wins). An unknown surface value gets a 400. router and core_router need the find_tool and call_tool handlers (issue #8757) and the core set (issue #8759); until both land they are not usable, and the default full is unaffected. Allowed: `full`, `router`, `core_router`. |
| `ESHU_MCP_TRANSPORT` | enum | `http` | MCP server transport mode. Allowed: `http`, `stdio`. |

## postgres

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_DEFER_CONTENT_SEARCH_INDEXES` | bool | `false` | Cold-bootstrap schema mode. When true, schema bootstrap creates the content tables without the four exact substring trigram GIN indexes (file content and relative path, entity source and name); bootstrap-index restores and validates them after source-local content projection drains. Existing indexes are never dropped. Keep false unless bootstrap-index is guaranteed to run to successful finalization. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | duration | `10m` | Idle timeout before a connection is closed. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | duration | `30m` | Connection lifetime before recycling. |
| `ESHU_POSTGRES_DSN` | dsn | — | Postgres connection string. DSN precedence is ESHU_FACT_STORE_DSN, then ESHU_CONTENT_STORE_DSN, then ESHU_POSTGRES_DSN. Aliases: `ESHU_FACT_STORE_DSN`, `ESHU_CONTENT_STORE_DSN`. |
| `ESHU_POSTGRES_EXPECTED_SYSTEM_ID` | string | — | Optional independently supplied physical PostgreSQL cluster system ID for API/MCP writer and reader topology validation; omitted checks configured endpoint agreement only. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | int | `10` | Maximum idle Postgres connections (capped at max open). |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | int | `30` | Maximum open Postgres connections. |
| `ESHU_POSTGRES_PING_TIMEOUT` | duration | `10s` | Timeout for the startup/readiness connectivity ping. |
| `ESHU_POSTGRES_READ_DSN` | dsn | — | Optional API/MCP read-only PostgreSQL endpoint; omitted uses the writer DSN with a separate read-only session pool. |
| `ESHU_POSTGRES_READ_MAX_IDLE_CONNS` | int | — | Optional API/MCP reader idle allocation from ESHU_POSTGRES_MAX_IDLE_CONNS; omitted defaults near half while respecting both pool limits. |
| `ESHU_POSTGRES_READ_MAX_OPEN_CONNS` | int | — | Optional API/MCP reader open allocation from ESHU_POSTGRES_MAX_OPEN_CONNS; omitted defaults to half and the writer gets the remainder. |
| `ESHU_POSTGRES_READ_MEMBERS` | string | — | Optional API/MCP nonempty JSON array of direct physical reader members (id, host, port). Uses shared credentials and TLS from ESHU_POSTGRES_READ_DSN; startup validates members and per-member pool capacity. One member has no reader redundancy. Omit for legacy reader Service routing. |
| `ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS` | string | — | Optional API/MCP nonempty JSON array of {server_version, evidence} objects that qualifies PostgreSQL reader builds below 18.7 for shared-snapshot export (#7957). server_version must equal current_setting('server_version') exactly (set --with-extra-version on the qualified image; a bare label like 18.6 is accepted with a startup warning); evidence is nonempty text up to 256 characters (image digest, package revision, or recipe reference). Unset is deny-by-default: an undeclared reader below 18.7 answers shared-snapshot code-topic reads with a permanent 503. Empty arrays, duplicates, and missing evidence fail startup. |
| `ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT` | duration | `3m` | How long a schema bootstrap (db-migrate, bootstrap-index) waits for another bootstrapper that owns the Postgres schema advisory lock before failing; unset keeps the default, a set value must be positive (#6956). |
| `ESHU_SCHEMA_LOCK_RETRY_BUDGET` | duration | `3m` | Retry allowance shared across one schema-bootstrap run after lock_timeout (SQLSTATE 55P03): failed-attempt time and actual backoff consume it; successful migration execution time does not. It stops further retries once spent and is not a strict server-side wait cap because PostgreSQL applies lock_timeout per lock acquisition; unset keeps the default, a set value must be positive (#6956). |
| `ESHU_SCHEMA_LOCK_TIMEOUT` | duration | `5s` | Per-statement lock-acquisition bound (Postgres lock_timeout) each schema-bootstrap migration statement applies before retrying with SQLSTATE 55P03; a bare CREATE/DROP INDEX CONCURRENTLY statement still runs with lock_timeout disabled (#7004). A longer value lets DDL queue for a lock gap instead of failing fast; every failed attempt still consumes ESHU_SCHEMA_LOCK_RETRY_BUDGET. Unset keeps the default; a set value must be positive (#6738). |

## projector

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for projector work items. |
| `ESHU_PROJECTOR_RETRY_DELAY` | duration | `30s` | Delay between projector work-item retries. |
| `ESHU_PROJECTOR_WORKERS` | int | — | Concurrent projector workers (default NumCPU capped at 8, min 1). |

## push-hooks

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_DOCS_BUILD_TIMEOUT_SECONDS` | int | `600` | Bound in seconds for the docs-build pre-push hook (scripts/verify-docs-build-changed.sh): past the bound the hook fails closed naming the live-gate holder. Integer 1 through 3600; unset keeps 600 (#7983). |

## reducer

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` | bool | `true` | Run the #7584 activation obligation consumer in the resolution engine. It settles the exact-generation obligations ProjectorQueue.Ack writes by running the partition-scoped deferred maintenance pass on the owed (scope, generation) only when its backward-evidence phase is missing, then waking the deployment_mapping rows waiting on it. On by default since the #7638 flip; set false to restore the old behavior, under which obligations stay pending until generation retention removes their generation. |
| `ESHU_ACTIVATION_OBLIGATION_LEASE` | duration | `30m` | How long one activation obligation claim holds its row (#7638 item 2, arbiter-ruled 30m interim). Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. The interim 30m default fits the measured 395 s full-corpus (984-scope) pass inside the lease-minus-a-fifth deadline; lower back only when a rewrite re-measures the pass p99 far below the deadline. |
| `ESHU_ACTIVATION_OBLIGATION_POLL_INTERVAL` | duration | `10s` | Wait after an activation obligation cycle that claimed nothing. Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. |
| `ESHU_ACTIVATION_OBLIGATION_WORKERS` | int | `1` | Concurrent activation obligation claim loops in one reducer process; workers claim through SKIP LOCKED so they never share work. Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. Values above 1 are unprobed; keep 1 until full-corpus evidence lands (#7638 item 3). |
| `ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE` | int | `10` | Scopes with no activation journal row whose retained generation chain one changed-since cycle backfills. |
| `ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS` | int | `5` | Counting failures (statement_timeout, connection_lost, sql_error, internal) of one activation before it becomes a link_poisoned chain break: the cursor advances, the scope keeps its state, and the next full generation links from it. Retries back off min(30m, 30s*2^(n-1)); lock misses and busy slots never count. |
| `ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL` | duration | `5s` | Wait between changed-since link cycles when a cycle was idle, retried, or failed. A cycle that linked or broke anything starts the next at once. |
| `ESHU_CHANGED_SINCE_LINK_SLOTS` | int | `2` | Database-wide cap on concurrent full changed-since links (root and incremental), held as transaction advisory locks shared by every reducer replica. Each full link of a 0.77M-key scope holds up to about 1 GiB of Postgres backend memory at work_mem 256MB. Raise only with the 1/2/4-link cap evidence. |
| `ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT` | duration | `120s` | statement_timeout of one changed-since link statement; the whole link transaction is also bounded by this value plus 30s. A timeout rolls back and is a counting failure (failure class statement_timeout): it is recorded on the scope cursor with backoff and, at ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS, becomes a link_poisoned chain break. |
| `ESHU_CHANGED_SINCE_LINK_WORKERS` | int | `4` | Scopes one reducer links at once. Full links are further bounded by ESHU_CHANGED_SINCE_LINK_SLOTS across all replicas. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` | string | `code-call-projection-runner` | Prefix for the code-call partition lease owner. The reducer appends hostname, PID, and a boot nonce so replicas and restarted processes never share one active owner identity. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT` | int | `500` | Maximum stale value-flow evidence nodes or edges deleted per active scope and family in one reducer cleanup pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED` | bool | `true` | Enable the reducer side runner that removes stale CodeTaintEvidence nodes and TAINT_FLOWS_TO edges from older active-scope generations. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER` | string | — | Lease owner for the single value-flow stale cleanup worker; defaults to a unique process token. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL` | duration | `10m` | TTL for the value-flow stale cleanup partition lease. Keep above the graph write budget (the QA environment's ESHU_CANONICAL_WRITE_TIMEOUT is 300s) plus margin (#7047). |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL` | duration | `1h` | Delay between value-flow stale cleanup passes. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT` | int | `100` | Active repository scopes scanned per value-flow stale cleanup pass. |
| `ESHU_GENERATION_LIVENESS_ACTIVATION_DEADLINE` | duration | `30m` | Age past which an active generation with outstanding shared intents and no unresolved reducer work can count as wedged. |
| `ESHU_GENERATION_LIVENESS_BATCH_LIMIT` | int | `200` | Maximum generations the sweep re-drives in one poll. |
| `ESHU_GENERATION_LIVENESS_ENABLED` | bool | `true` | Runs the generation-liveness sweep in the resolution engine. When false the sweep neither re-drives wedged active generations nor supersedes orphaned older active generations; the stuck gauge bucket still reports. |
| `ESHU_GENERATION_LIVENESS_MAX_RECOVER_ATTEMPTS` | int | `5` | Per-generation re-drive budget; a generation at the ceiling is no longer re-driven by the sweep. |
| `ESHU_GENERATION_LIVENESS_POLL_INTERVAL` | duration | `5m` | How often the generation-liveness sweep runs. |
| `ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW` | duration | `10m` | Quiet period a blocking shared-intent projection_domain queue must show before the generation-liveness sweep re-drives a wedged active generation; generations whose domain queue completed work inside it are counted draining and skipped. Raised to the liveness poll interval when set lower. |
| `ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE` | duration | `2160h` | Hard history ceiling for superseded generations (#7585): ordinary superseded history older than this is eligible for bounded cleanup even when the retained count (ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS) would keep it. Unset resolves to 2160h, or to ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE when that is longer (#7611); an explicit value below ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE is rejected at startup. |
| `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE` | duration | `168h` | Soft keep window for superseded generations: newer generations stay retained. The hard history ceiling must not be set below this. |
| `ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS` | int | `24` | Minimum superseded generations retained per scope after the active one; the hard history ceiling (ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE) still applies past it. |
| `ESHU_GENERATION_RETENTION_SEARCH_INDEX_SUPERSEDED_SWEEP_ENABLED` | bool | `false` | Opts into the superseded search index sweep (#8231): a still-existing superseded generation's search index rows (terms, documents, and stats tables) are removed in committed batches while its facts stay held. Off by default; with the switch off the sweep issues no SQL. Enablement stays held until the issue's gate-(b) primary dead-tuple/bloat evidence lands. |
| `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` | duration | `5m` | Delay between background refreshes of the graph-backed observable gauges (eshu_dp_edges_by_source_tool, eshu_dp_files_by_language, eshu_dp_graph_orphan_nodes). /metrics serves the last snapshot, so while refreshes succeed those gauges lag the graph by up to this interval plus one read. If refreshes keep failing the gauges stop reporting once the snapshot is older than three intervals; eshu_dp_gauge_snapshot_age_seconds shows the age. |
| `ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT` | duration | `30s` | Deadline for each background graph read that refreshes a graph-backed observable gauge. A read that exceeds it is cancelled and counted as a timeout in eshu_dp_gauge_snapshot_refreshes_total; the previous snapshot keeps being served until it is three intervals old. |
| `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT` | int | `100` | Maximum graph orphan nodes deleted per label in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` | int | `10000` | Maximum graph orphan nodes counted per label for telemetry in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED` | bool | `true` | Enable the reducer side runner that marks and sweeps stale generation-owned graph orphans. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER` | string | — | Lease owner for the single graph orphan sweep worker; defaults to a unique process token. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL` | duration | `10m` | TTL for the graph orphan sweep partition lease. Keep above the graph write budget (the QA environment's ESHU_CANONICAL_WRITE_TIMEOUT is 300s) plus margin (#7047). |
| `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL` | duration | `1h` | Delay between graph orphan sweep passes. |
| `ESHU_GRAPH_ORPHAN_SWEEP_TTL` | duration | `168h` | Minimum age before a marked graph orphan can be deleted. |
| `ESHU_ID_ANCHOR_CENSUS_ENABLED` | bool | `true` | Runs the periodic id-anchor census (#7212) on Neo4j: one AllNodesScan that counts graph nodes with an id the labeled entity-context anchor cannot reach, reported as eshu_dp_graph_id_anchor_unreachable_nodes. No effect on NornicDB. |
| `ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` | duration | `1h` | Delay between id-anchor census passes. The first pass runs at startup. |
| `ESHU_ID_ANCHOR_CENSUS_TIMEOUT` | duration | `2m` | Deadline for one id-anchor census pass. A pass that exceeds it counts as failed and keeps the last good snapshot. |
| `ESHU_INFRA_INVENTORY_RECONCILE_ENABLED` | bool | `true` | Run the infra read model reconcile loop, which re-derives repositories whose infra_resource_entities rows drifted from content_entities, and repairs rolling-upgrade fence marks. With it false, nothing repairs rolling-upgrade fence marks: one write from an older binary or manual SQL keeps unscoped infra aggregate reads on the graph until the loop runs again (see eshu_dp_infra_inventory_dirty_repos and the infra_inventory admin status field). |
| `ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL` | duration | `5m` | Wait between infra read model reconcile cycles. |
| `ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET` | int | `500` | Repositories one infra read model reconcile cycle checks. |
| `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL` | duration | `5m` | Delay between background refreshes of the Postgres-backed observable gauges (queue depth/age, shared acceptance rows, workflow family queue depth, active generations, poison liveness), read by the reducer and the ingester. /metrics serves the last snapshot, so while refreshes succeed those gauges lag Postgres by up to this interval plus one read. If refreshes keep failing the gauges stop reporting once the snapshot is older than three intervals; eshu_dp_gauge_snapshot_age_seconds shows the age. |
| `ESHU_POSTGRES_GAUGE_REFRESH_TIMEOUT` | duration | `30s` | Deadline for each background Postgres read that refreshes a Postgres-backed observable gauge. A read that exceeds it is cancelled and counted as a timeout in eshu_dp_gauge_snapshot_refreshes_total; the previous snapshot keeps being served until it is three intervals old. |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | int | `10000` | Ingester source-local reducer-intent admission threshold; defers while outstanding reducer queue depth is at or above this value. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | duration | `1s` | Queue-depth recheck interval while reducer admission is deferring. |
| `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` | int | `500` | Ingester graph-write backpressure: defers source-local reducer-intent admission while retrying-state reducer depth (the durable signal of recurring graph-write timeouts) is at or above this value, so recoverable work is throttled instead of dead-lettered. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` | int | `100` | Hysteresis floor for graph-write backpressure; admission resumes only after retrying-state reducer depth falls below this value. Must be less than ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | int | — | Work items claimed per cycle (default adaptive to workers and backend). |
| `ESHU_REDUCER_CLAIM_DOMAIN` | string | — | Single reducer claim domain. Deprecated; use `ESHU_REDUCER_CLAIM_DOMAINS`. |
| `ESHU_REDUCER_CLAIM_DOMAINS` | string | — | Comma-separated reducer domains for multi-domain claims. |
| `ESHU_REDUCER_HANDLER_TIMEOUT` | duration | — | Default bound on one reducer handler execution. Recorded but not enforced yet (#8211 step a). Invalid values fail startup. |
| `ESHU_REDUCER_HANDLER_TIMEOUT_OVERRIDES` | string | — | Per-domain reducer handler bounds as domain=duration,... . Recorded but not enforced yet (#8211 step a). Bad entries and duplicate domains fail startup. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for reducer work items. |
| `ESHU_REDUCER_RETRY_DELAY` | duration | `30s` | Delay between reducer work-item retries. |
| `ESHU_REDUCER_WORKERS` | int | — | Concurrent reducer workers (default derived from CPU count and backend). |
| `ESHU_SHARED_PROJECTION_LEASE_OWNER` | string | `shared-projection-runner` | Prefix for the shared-projection partition lease owner. The reducer appends hostname, PID, and a boot nonce so replicas and restarted processes never share one active owner identity. |
| `ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX` | duration | `30s` | T_max for per-partition shared-projection backoff: the longest a persistently unproductive (domain, partition) waits between visits, and the pickup bound for work arriving during backoff (within T_max plus one global poll interval, at most 5s). Clamped to 5m; empty or invalid values fall back to the default. |
| `ESHU_STATUS_SUMMARY_WRITER_ENABLED` | bool | `false` | Run the reducer's periodic status summary writer (#7009), which stores the active-work summary and the Terraform-state admin evidence (two rows in one pass, each in its own transaction) in status_summary_snapshots every ESHU_STATUS_SUMMARY_WRITER_INTERVAL. One replica computes per tick under a transaction advisory lock; the others skip. Off by default; with it off the reducer starts no writer and issues no writer SQL. Nothing reads the row until the status reader flag ships. |
| `ESHU_STATUS_SUMMARY_WRITER_INTERVAL` | duration | `10s` | Cadence of the status summary writer. The 10s default follows the 1.0s median active-work pass measured on the QA read replica. Values below 5s, or unparsable values, fail reducer startup: a 2s cadence measurably raised reducer claim latency. A pass longer than the interval is counted in eshu_dp_status_summary_writer_overrun_total and the next pass starts on the following interval boundary; passes never overlap. |

## runtime

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED` | bool | `false` | Answer get_changed_since from the changed-since link ledger when a link chain covers the requested window (#7127 PR-3c). Read by the API and the MCP server through one parse (1, true, t, yes, y or on is on; anything else is off). The read tries one link, then a chain of links under a 200k stored-delta-row budget, then the legacy two-generation diff; every miss is counted on eshu_dp_changed_since_ledger_reads_total by outcome and the wire contract never changes. Off by default; with it off the read path issues no ledger SQL. Do not enable in any environment until BIN1 (#7401) passes and the writer is enabled and measured. |
| `ESHU_CHANGED_SINCE_LINK_ENABLED` | bool | `false` | Turn on the dark changed-since link ledger (#7127). Read by three runtimes, all through one parse (1, true, t, yes, y or on is on; anything else is off). The reducer runs the changed_since_link domain, which journals generation activations and links each one into the changed-since ledger tables. The ingester and bootstrap-index make the projector Ack journal the activation it commits (source ack) in the same transaction, only while it holds the shared side of the journal pass's try-only advisory key, so it never waits; a missed key leaves the activation to the reducer's sweeper. Off by default; with it off none of them issues ledger SQL. The ledger is read only under ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED (PR-3c). Enable only after generation retention cleans the ledger tables (PR-3d), and do not enable it on the ingester in any environment until a built-binary run with the Ack journal, the journal pass, the link writer and retention all on passes (#7401). |
| `ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED` | bool | `true` | Kill switch of the config-artifact derive (#8155) that the content Write runs as its last step in the ingester, the projector, and bootstrap-index. Read once at process start. With it false the binary builds no deriver (no derive call, metric, or log per Write) and logs one INFO record with event_name=config_artifacts.derive.disabled at start; stored config-artifact rows then fall behind the content version, readers fall back to the live read for those repositories, and the reducer walker, once it lands, repairs them. It gates only the Write derive; the walker gets its own variable. An unparsable value fails startup. Read by the ingester, the projector, and bootstrap-index at start (#8155 PR3b-2c-2 wires the derive). |
| `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT` | duration | `10s` | Deadline for one per-repository config-artifact derive (#8155) in the ingester, the projector, and bootstrap-index, and later the reducer walker. The failure record runs after the deadline on its own 5s bound, so a Write can wait up to this value plus 5s (15s by default). Read once at process start. 0, a negative value, or an unparsable value fails startup; 0 never means no deadline. A value below the time the derive needs for your largest repositories turns those Writes into recorded timeout failures. Read by the ingester, the projector, and bootstrap-index at start (#8155 PR3b-2c-2 wires the derive). |
| `ESHU_CONFIG_ARTIFACT_READ_MODE` | enum | `live` | Config-artifact read mode of the API (#8155 PR3c). live serves the query-time extractor for every repository, as today. shadow serves the live answer and compares each related repository against its stored rows, counting eshu_dp_config_artifact_shadow_compare_total{outcome} without changing the response. stored serves the stored rows of trusted repositories with a live fallback per repository. Read once at API start. Unset or blank means live; any other value fails startup. Rollback stays a flag flip to live with an API roll. Allowed: `live`, `shadow`, `stored`. |
| `ESHU_CONFIG_ARTIFACT_WALKER_ENABLED` | bool | `true` | Kill switch of the config-artifact reducer walker (#8155 PR3b-2): the loop that repairs dirty repositories and backfills the stored rows before the PR3c read cutover. Read once at reducer start. With it false the reducer builds no walker and issues no walker statement; stored rows then fall behind the content version until the content Writes' own derives or a later walker run repairs them, and the backfill marker never records. An unparsable value fails startup. Read by the reducer at start. |
| `ESHU_CONFIG_ARTIFACT_WALKER_WORKERS` | int | `4` | Derive workers per process of the config-artifact reducer walker (#8155 PR3b-2): K workers partition each cycle's repositories by repo_id. Read once at reducer start. Below 1 or unparsable fails startup. Size it against the per-repository derive cost (S1: milliseconds typical, about 0.2s for a 5,000-file repository) times the repositories each cycle derives. Read by the reducer at start. |
| `ESHU_LISTEN_ADDR` | string | `0.0.0.0:8080` | Primary HTTP listen address (host:port). |
| `ESHU_METRICS_ADDR` | string | `0.0.0.0:9464` | Prometheus metrics listen address (host:port). |
| `ESHU_PPROF_ADDR` | string | — | Opt-in pprof profiler address; unset disables it; a port-only value binds to 127.0.0.1. |
| `ESHU_STATUS_SUMMARY_READ_ENABLED` | bool | `false` | Make every status snapshot read the stored active-work summary row and Terraform-state evidence row (#7009) instead of running the live statements, in the API, the MCP server, and every runtime that builds a status store. Off by default; with it off no summary SQL runs. Each row is read inside the status snapshot transaction and decided on its own. A missing table or row, a row from another statement version, a row older than ESHU_STATUS_SUMMARY_STALE_AFTER, or an undecodable row runs that model's live statement or statements for the whole answer, counted in eshu_dp_status_summary_read_total with a reason and reported in the payload's active_work_source object and, on the routes that render the Terraform-state section, its terraform_state_source object. A database error reading the row fails the status read. The runtime /metrics scrape of every hosted runtime never runs a live statement and reads only the active-work row: a stale or missing row serves the newest row that process can decode (a stale one included, with its true age), or an empty summary when it can decode none, and exports eshu_runtime_status_summary_stale 1 and eshu_runtime_status_summary_age_seconds, counted in eshu_dp_status_summary_scrape_total. Read once when the process starts. Needs ESHU_STATUS_SUMMARY_WRITER_ENABLED on the reducer, or every read falls back as missing. |
| `ESHU_STATUS_SUMMARY_STALE_AFTER` | duration | `33s` | Oldest age at which a stored summary row (active work or Terraform state) is still served; read only while ESHU_STATUS_SUMMARY_READ_ENABLED is on. The 33s default is three writer intervals of 10s plus the 2.09s replay-lag p95 measured on the QA read replica, rounded up. Values below 10s, or unparsable values, fail process startup while the reader is on (the settings are read once at startup): a smaller bound sends most reads to the slower live fallback. Age is the reader's database clock minus the row's as_of. Keep it above 3 x ESHU_STATUS_SUMMARY_WRITER_INTERVAL plus the replica replay lag. |

## semantic

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | string | — | JSON semantic extraction policy controlling source/provider ACL decisions. |
| `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` | bool | `false` | Default-off flag permitting real provider traffic (requires security review). |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | string | — | JSON array of semantic provider profile configurations, including optional search embedding dimensions. |
| `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` | bool | `false` | Enable the semantic-provider worker claim loop. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | enum | — | Deterministic no-network or auto-local semantic search selector for API, MCP, and reducer. Allowed: `hash`, `local_hash`, `auto_hash`. |
| `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` | string | — | Selects one governed search_documents provider profile when multiple semantic search providers are configured. |
