# GitLab Group Discovery Source Mode (#7765)

Status: proposed. Implements the #7765 obligations of the #7765/#7766 arbiter
ruling (Option C) within its non-goals. Source check: origin/main 7be568e44,
2026-10-09. Depends on PR #7821 (token auth host scope, open, head f727f0080)
and consumes the GitLab host through one function that #7764 will make
configurable.

This page holds the decision and the contracts. The prove-first plan, test plan
and PR breakdown are in
[the proof and rollout page](7765-gitlab-group-discovery-proof-and-rollout.md).

## Decision

Add `ESHU_REPO_SOURCE_MODE=gitlabGroup`. It lists every project under one
GitLab group, including subgroups, through `GET /groups/:id/projects`. It
selects those projects with the existing rule and archive policy, syncs them as
`gitlab/<path_with_namespace>` repository IDs, and records selection
observations under a new membership kind, `gitlab_group`.

The membership partition is generalized first, in its own PR. Today known
scopes are matched on the first slug segment. A `gitlab_group` selector instead
matches the path prefix `<group full path>/`. `github_org` and `explicit` keep
their current SQL text, Go predicate and selector IDs byte for byte.

#7765 writes `repository_selection_observations` rows and nothing else. It never
writes the graph, and it never retires, hides or deletes anything.

### Corrections to the arbiter ruling

The ruling was verified read-only. Four of its premises were imprecise against
origin/main, and this design follows the code on each:

1. **Gauge label.** The ruling puts `selector_kind="gitlab_group"` on the counter
   and the gauge. The gauge has no such label (`observer.go:184-187`) and is
   sampled only for `github_org` (`observer.go:174`). The label goes on the
   counter only, and the gauge condition widens to `gitlab_group`.
2. **Keyset pagination.** Keyset paging is documented for `GET /projects`, but
   the "Supported resources" table at https://docs.gitlab.com/api/rest/ does not
   list `GET /groups/:id/projects`. This design uses offset paging with
   `order_by=id&sort=asc`.
3. **Listing completeness.** The ruling ends a listing on an empty page or an
   absent `rel="next"`. The GitHub code ends only on an empty page, and an absent
   `rel="next"` on a non-empty page does not end it (`selection_github.go:293-296`).
   The design mirrors the code.
4. **Host through `KnownScopeHost`.** `membership` is a leaf and must not import
   `git` (`membership/AGENTS.md`), so `KnownScopeHost("gitlab_group")` cannot call
   `repoProviderHost`. The git package passes `repoProviderHost("gitlab")` in
   `Selector.Host`, hashed with `omitempty` so `github_org` and `explicit` IDs
   stay byte-identical.

Two items on the ruling's "Not verified" list are closed. Migration 166
constrains `selector_kind` (`166_repository_selection_observations.sql:32`), so
a new guarded migration is required. `githubOrg` does no fork filtering
(`selection_github.go:262` uses `type=all`; `:280-284` decodes no fork field).

## Current contract this builds on

- **Source modes.** `discoverSelection` handles `filesystem`, `explicit` and
  `githubOrg` (`selection_github.go:162-201`).
  `SelectRepositories` treats a token error as fatal only for `githubOrg`
  (`selection_native.go:55-58`), and so does `syncGitRepositoriesWithLogger`
  (`selection_cli.go:40-43`). It observes before sharding (`selection_native.go:68-69`).
  It syncs git modes in `case "explicit", "githubOrg"` (`selection_native.go:136`).
- **GitHub listing.** It pages at a fixed 100 (`selection_github.go:236`) with
  `type=all` (`:262`) and decodes only `id`, `full_name` and `archived` (`:280-284`).
  Only an empty page ends it, and it is complete only if that page has no
  `rel="next"` (`:293-296`). Reaching `RepoLimit` always makes it incomplete
  (`:309-311`). **It does no fork filtering**, so GitLab forks inside the group
  are listed too.
- **Selection.** `selectGitHubRepositoryIDs` (`selection_discovery.go:90-157`)
  applies the archive policy. An exact rule re-admits an archived repository
  (`:112-113`). Rules then split the rest into selected and rule-excluded.
  Exact rules match case-sensitively after `normalizeRepositoryID`
  (`selection_config.go:28-42`, `:372-389`). That function already handles any
  path depth.
- **Repository identity.**
  - `repoProviderAndSlug` maps `gitlab/<a>/<b>/...` (3 or more segments) to
    provider `gitlab` (`selection_discovery.go:309-321`).
  - `repoProviderHost("gitlab")` is hardcoded to `gitlab.com` (`:323-332`).
  - `repoRemoteURL` builds `https://<host>/<slug>.git` (`:292-307`).
  - The scope ID is `git-repository-scope:` plus
    `CanonicalRepositoryID(NormalizeRemoteURL(remote))`
    (`source_processing.go:366-398`, `repositoryidentity/identity.go:248-259`).
  - `NormalizeRemoteURL` lowercases both host and path
    (`repositoryidentity/identity.go:84-91`). So `repo_slug` is the lowercased
    `path_with_namespace` (`:225-236`).
  - `gitScopeIdentityForRepositoryID` derives the scope ID and slug without a
    checkout (`selection_baseline.go:426-439`).
- **Membership.**
  - `KindGitHubOrg`/`KindExplicit` are at `membership/identity.go:17-24`.
  - `KnownScopeHost` returns `github.com` only for `github_org` (`:30-40`).
  - The selector hash covers version 2, kind, source mode, lowercased owner,
    normalized sorted rules and include-archived (`:76-83`, `:123-158`).
  - `partitionKnown` keeps scopes whose first slug segment equals the owner
    (`evaluate.go:227-250`).
  - The store reads `lower(split_part(payload->>'repo_slug','/',1)) = $1` plus
    an optional host predicate (`storage/postgres/membership/observations.go:50-58`).
  - The store accepts only the two kinds (`observations.go:233`).
  - Migration 166 constrains `selector_kind IN ('github_org','explicit')`
    (`migrations/166_repository_selection_observations.sql:32`).
- **Telemetry and webhooks.** The gauge `eshu_dp_collector_repository_selection_scopes`
  has no `selector_kind` label (`telemetry/instruments_repository_selection.go:70-81`).
  A GitLab webhook trigger maps to `gitlab/` + `path_with_namespace`
  (`webhook_trigger_selector.go:298-311`), the ID shape this mode uses.

## Configuration

| Variable | Meaning in `gitlabGroup` |
| --- | --- |
| `ESHU_REPO_SOURCE_MODE=gitlabGroup` | New mode. |
| `ESHU_GITLAB_GROUP` (new) | Required. The group's full path (`acme/platform`) or numeric ID. Every cycle resolves it with `GET /groups/:id?with_projects=false` to `full_path`, which becomes the partition and selector owner (lowercased). A 404 fails the cycle (`group_not_found`). |
| `ESHU_INCLUDE_ARCHIVED_REPOS` | Reused (`selection_config.go:158`). |
| `ESHU_REPOSITORY_RULES_JSON` | Reused. Exact and regex rules both allowed, like `githubOrg`. |
| `ESHU_REPO_LIMIT` | Reused (default 4000, `selection_config.go:140`). |
| `ESHU_GIT_AUTH_METHOD` | Must be `token`. Config load rejects `githubApp` (it mints a GitHub token, `selection_github.go:213-214`), `ssh` and `none`. |
| `ESHU_GIT_TOKEN` | Required. **`GITHUB_TOKEN` is not accepted as a fallback in this mode** (`selection_config.go:159` reads both). That fallback would send a GitHub credential to gitlab.com. |

An exact rule must start with `gitlab/<group full path>/`, compared
case-insensitively. One that does not could never match, and every project would
silently read `rule_excluded`. Where the check runs depends on the group's form:

- A numeric ID has no known prefix at config load, so the authoritative check
  runs in `discoverSelection` right after `ResolveGroup`, against the resolved
  `full_path` and before the project listing. A mismatch fails the cycle with
  `failure_class=config_invalid` and writes no rows.
- A path-form group may also be checked at config load, as an early error with
  the same message.

Regex rules match the full `gitlab/...` ID and cannot be checked statically, so
the docs say so.

To keep the git package's files small, the config-load checks
(`gitlab.ValidateConfig`) and the post-resolve prefix check
(`gitlab.CheckExactRules`) live in the new leaf (see "GitLab listing client").
`LoadRepoSyncConfig` calls the first when the mode is `gitlabGroup`.

**SSH is rejected.** Cloning over SSH would still need a separate API credential
for the listing, and the chart renders `ESHU_GIT_TOKEN` only for `token` auth
(`deploy/helm/eshu/templates/statefulset.yaml:139-140`). The one-credential model
also keeps the selector principal identical to the clone credential.

**Env registry, Helm and Compose.** The registry entries, chart changes and
compose note are listed on
[the proof and rollout page](7765-gitlab-group-discovery-proof-and-rollout.md#rollout-surfaces).

## GitLab listing client

**Location.** A new leaf package, `go/internal/collector/repo/git/gitlab`
(`git -> leaf`, never the reverse). The git directory sits in the dirgate ratchet
at exactly 65 files (`scripts/lib/dirgate-grandfather.tsv:79`), so a new file
there fails `go-dir-gate`. Its nearby files are close to the 500-line cap: 420
in `selection_github.go`, 441 in `selection_discovery.go`, 445 in
`selection_config.go`. The leaf needs `doc.go`, `README.md` and `AGENTS.md`.

**Surface.**
- `ResolveGroup(ctx, client, apiBase, group, token) (Group{ID, FullPath}, error)`
- `ListGroupProjects(ctx, client, apiBase, fullPath, limit, token) (Listing, error)`
  - `Listing{Projects []Project, Complete bool, Pages int, OutsideGroup int}`
  - `Project{ID int64, PathWithNamespace string, Archived bool}`
- `CheckExactRules(fullPath string, exactRuleIDs []string) error`, the
  post-resolve prefix check from "Configuration" (`config_invalid`)

`apiBase` is `"https://" + repoProviderHost("gitlab") + "/api/v4"`, built in the
git package. That keeps one host function, which #7764 replaces.

**Request.**
`GET {apiBase}/groups/{url.PathEscape(fullPath)}/projects?include_subgroups=true&with_shared=false&order_by=id&sort=asc&per_page=100&page=N`

- `include_subgroups` defaults to `false` and `with_shared` defaults to `true`
  ([Groups API, list a group's projects](https://docs.gitlab.com/api/groups/)).
- `:id` accepts an ID or a URL-encoded path, with `/` sent as `%2F`
  ([REST namespaced paths](https://docs.gitlab.com/api/rest/)).
- **`archived` is omitted.** Both archived and active projects come back, and
  the `archived` field is classified locally into `archived_excluded`, as in
  GitHub.
- **`min_access_level` is not set.** With it, the listing would depend on the
  token's role, and a role downgrade would read as a mass departure (the guard
  false positive the membership README describes). A visible project that cannot
  be cloned shows up as an ordinary per-repository clone failure, not a silent
  omission.
- **`simple=true` is not used.** Whether it returns `archived` is NOT_CHECKED.

**Defensive namespace check.** Any project whose lowercased `path_with_namespace`
does not start with `lower(full_path)+"/"` is dropped and counted in
`OutsideGroup`. Shared-in projects carry a different `namespace`, per the same
docs page, so they never enter the listing even if `with_shared` were ignored.

**Pagination: offset, ordered by `id asc`.**
- Keyset pagination is not documented for `/groups/:id/projects` (see
  "Corrections to the arbiter ruling").
- `per_page` maxes at 100 and is fixed for the whole listing (same reason as
  `selection_github.go:232-236`).
- Ordering by ascending `id` puts new projects at the end, so a creation during
  the listing never shifts earlier pages. It also makes `ESHU_REPO_LIMIT`
  truncation deterministic: it keeps the oldest projects.
- A deletion during the listing can shift one project onto an already-read page.
  That project misses one cycle and gets a pending `not_listed` row. Confirmation
  needs at least 2 cycles spanning 5 minutes (membership README "Confirmation"),
  so it never confirms.
- GitLab omits `x-total` past 10,000 records, so completeness never uses it.

**Completeness mirrors `listGitHubOrgRepositoriesFrom` exactly.**
- Only an empty page ends the listing. A short page mid-listing does not.
- `Complete` is true only if that empty page's `Link` header has no
  `rel="next"` (reusing `linkHeaderHasNext`, which moves to the leaf or is
  duplicated there with a parity test) and the listed count is below the limit.
- Reaching the limit is always incomplete and is trimmed to the limit, so the
  observer reports `listing_truncated` and writes nothing.

**Authentication.**
- Header `PRIVATE-TOKEN: <token>`, the documented recommended form
  ([REST authentication](https://docs.gitlab.com/api/rest/authentication/)).
- Required scopes are `read_api` (listing) and `read_repository` (clone over
  HTTPS) ([token scopes](https://docs.gitlab.com/security/tokens/access_token_scopes/)).
- Recommend a group access token: its `read_repository` covers "all
  repositories in the group".
- Clone auth is PR #7821 (head f727f0080): the HTTP Basic extraheader is scoped
  to the clone host. For `gitlab/...` checkout paths that is gitlab.com with
  username `oauth2` (`tokenAuthProvider` and `tokenAuthUsername` in that PR's
  `selection_ssh.go`). Token auth fails closed, with no header, only for a path
  outside `ReposDir` or inside `.eshu-ref-worktrees`. A managed path with no
  provider prefix still resolves to github, which the webhook guard covers. GitLab accepts any non-empty username
  ([personal access tokens](https://docs.gitlab.com/user/profile/personal_access_tokens/)).
- **Without #7821, private projects clone anonymously and fail.** The wiring
  PR must merge after #7821.

**Rate limits and backoff.**
- gitlab.com allows "Group projects requests (`/api/v4/groups/:id/projects`)"
  600 per minute, and authenticated API traffic 2,000 per minute per user
  ([GitLab.com rate limits](https://docs.gitlab.com/user/gitlab_com/rate_limits/)).
- Throttled requests get `429` with `Retry-After` in seconds. Groups and Projects
  API responses do not include the informational `RateLimit-*` headers, so the
  client keys only on `429` and `Retry-After`.
- 4,000 projects is 41 requests per shard per cycle. N shards each list the
  group, so N×41 per cycle.
- Retry `429`, `502`, `503` and `504` up to 3 times per page. The wait is
  `Retry-After` (seconds or HTTP-date), or else 1s/2s/4s, clamped to [1s, 60s].
  It respects `ctx` and uses an injected sleeper for tests. Re-requesting an
  offset page is idempotent.
- Per-request timeout is 15s, as for GitHub (`selection_github.go:228`).

**Error classes.** Any error fails the cycle with no partial listing, as GitHub
does (`selection_github.go:287-289`). The class rides on `failure_class`.

| Class | Cause |
| --- | --- |
| `auth_failed` | 401 |
| `forbidden` | 403, usually the token lacks `read_api` |
| `group_not_found` | 404 |
| `config_invalid` | An exact rule outside the resolved group prefix |
| `rate_limited` | 429 after retries |
| `upstream_error` | Other 5xx after retries |
| `transport_error` | Network failure |
| `decode_error` | Response body did not decode |

## Repository IDs and scope identity

- The config and rule ID is `normalizeRepositoryID("gitlab/" + path_with_namespace)`.
  The case GitLab returns is kept, because the rules are case-sensitive.
- The checkout path is `<ReposDir>/gitlab/<path>`, so #7821's host derivation
  and `repoRemoteURL` agree.
- **The scope ID is not that string.** It is
  `git-repository-scope:repository:r_<8hex>` over
  `https://gitlab.com/<lowercased path>`.

Consequences, stated as contract:

1. **Case-only renames** keep the scope ID (lowercased), but exact rules must
   follow the new case.
2. **A project rename or move** (between subgroups, or out of the group) creates
   a new scope. The old scope reads `not_listed`, and after confirmation
   `not_selected`. It is a #7766 candidate. #7765 never acts on it.
3. **A rename or move of the configured group itself.**
   - Configured by path: the lookup 404s and every cycle fails until the
     operator updates `ESHU_GITLAB_GROUP`.
   - Configured by ID: the lookup returns the new `full_path`, so the owner and
     the selector ID change.
   - Either way, the old prefix's scopes are judged by nobody. Once the old rows
     outlive the liveness window they read selection `unknown`, not
     `not_selected`. This is a documented limit.
4. **A #7764 host change** (gitlab.com to a self-managed host) changes every
   GitLab scope ID and the selector ID (the host is hashed, see "Membership integration").
   Re-scoping is an open question for #7764, not something #7765 fixes.

## Membership integration

**Kind and selector.**
- Add `KindGitLabGroup = "gitlab_group"`.
- Add `NewGitLabGroupSelector(sourceMode, host, fullPath string, rules []Rule, includeArchived bool, principal string)`.
- `Selector` gains `Host string`, and `selectorIdentity` gains
  ``Host string `json:"host,omitempty"` ``.
  - `omitempty` leaves the canonical JSON for `github_org` and `explicit`
    unchanged, so their IDs are unchanged. A golden test pins them (PR 1).
  - Version stays 2.
- Owner is the lowercased resolved `full_path`.
- Principal is `selectionPrincipal(config)` (`selection_github.go:152-160`).
  That gives `TokenPrincipal` here. Anonymous is unreachable because config
  requires a token.

**Host.**
- `membership` is a leaf and must not import `git` (`membership/AGENTS.md`), so
  it cannot call `repoProviderHost` itself.
- The git package passes `repoProviderHost("gitlab")` into the selector.
- Add an unexported `(Selector) knownHost()`. It returns `s.Host` for
  `gitlab_group` and `KnownScopeHost(s.Kind)` otherwise.
- `KnownScopeHost` keeps its signature and its `github_org`/`explicit` results.
- A `gitlab_group` selector with a blank host fails closed. The outcome is
  `store_error` with `failure_class=known_scopes_read` and the message
  "gitlab_group selector has no host". It never runs a host-less read.

**Partition (PR 1).**
- Add `type Partition struct{ Owner, Host string; PathPrefix bool }` and
  `(Selector) Partition()`:
  - `github_org`: `{owner, "github.com", false}`
  - `explicit`: `{owner, "", false}`
  - `gitlab_group`: `{owner, host, true}`
- `Store.KnownScopes(ctx, Partition)` replaces `(ctx, owner, host)`. The one
  production caller is `observer.go:130`. The test fakes and direct callers need
  a mechanical signature update; their assertions do not change (the file list
  is in PR 1 on
  [the proof and rollout page](7765-gitlab-group-discovery-proof-and-rollout.md#pr-breakdown)).
- **Why not an optional prefix interface.** A store that lacks it would fall
  back to the first-segment read for a `gitlab_group` selector, the
  mass-`not_listed` hazard in a quiet form. One required signature makes the
  compiler reject a store that cannot serve a prefix partition.
- The Postgres store's `Partition` path keeps today's input rules: it trims and
  lowercases the owner and host and rejects a blank owner before any query
  (`observations.go:120-130`, pinned by `observations_test.go:34` and `:236`).
- In Go, `partitionKnown(known, Partition)`:
  - first-segment mode keeps today's `strings.EqualFold(slugOwner(slug), owner)`;
  - prefix mode uses `strings.HasPrefix(strings.ToLower(slug), owner+"/")`.
- In SQL, first-segment mode runs the existing `knownScopesQuery` constant
  unchanged. Prefix mode runs a separate constant:

  ```sql
  SELECT scope_id, payload->>'repo_slug'
  FROM ingestion_scopes
  WHERE source_system = 'git' AND scope_kind = 'repository'
    AND collector_kind = 'git'
    AND starts_with(lower(payload->>'repo_slug'), $1)
    AND ($2 = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = $2)
  ```

  - `$1` is `owner + "/"`.
  - `starts_with` instead of `LIKE` because `_` is a LIKE wildcard and legal in
    GitLab paths (`acme_x/` would match `acmeYx/`).
  - The trailing `/` keeps `acme/platform` from matching `acme/platformer`.
  - `repository_ref` scopes stay excluded.

**Rows.**
- `gitlab_group` writes a full row set like `github_org`: selected, excluded and
  not listed, with the mass-miss guard.
- `evaluateExplicit` stays `explicit`-only (`evaluate.go:143-144`).
- `validateBatch` admits `KindGitLabGroup` (`observations.go:233`) and keeps the
  explicit-only "selected rows only" check (`:252-254`).

**The `github_repo_id` column.**
- Store the GitLab project ID in it, as is. It already means "the selector's
  provider numeric ID".
- Its readers are the COALESCE in the upsert, `Observations`
  (`observations.go:91`, `:180`) and the carry-forward of a prior row's value
  (`observation.go:95-96`). Nothing interprets it.
- The new migration adds `COMMENT ON COLUMN` to say so.
- A rename is rejected (see "Risks, rejected alternatives, open questions").

**Migration.**
- Add `169_repository_selection_observations_gitlab_group.sql`. The highest
  migration on origin/main is 168 and no fetched remote branch holds 169, but
  the number is assigned at PR time. It uses the guarded `DO $$` pattern of `112_value_flow_refresh_producer_domains.sql`:
  if the constraint `repository_selection_observations_selector_kind_check`
  exists and its definition lacks `gitlab_group`, drop it and re-add it with the
  three kinds. Then the comment.
- Postgres default constraint naming is verified with `\d` before writing it.
- 166 and `schemaSQL` stay untouched. The parity test compares 166 only
  (`observations_test.go:265-279`).
- Live tests apply the full bootstrap (`observations_live_test.go:157`), so they
  see 169.

**Fork parity.** GitLab forks that live in the group are listed and selected
like any other project, matching GitHub's lack of fork filtering.

## Selector wiring, sharding, webhooks

- `discoverSelection` gains a `case "gitlabGroup"`. It calls `gitlab.ResolveGroup`
  then `ListGroupProjects`, maps the projects to `[]GitHubRepositoryRecord{RepoID,
  GitHubID: project.ID, Archived}`, and reuses `selectGitHubRepositoryIDs`
  unchanged. Setting `ListingComplete` and the resolved full path is about 25
  lines in `selection_discovery.go`, which ends near 466 lines.
- `RepositorySelection` gains `GroupFullPath string`.
- `githubOrgSelectionRequest` is generalized to `listingSelectionRequest(config,
  discovered, observedAt, selector)`.
  - `githubOrg` passes `NewGitHubOrgSelector(...)`.
  - `gitlabGroup` passes `NewGitLabGroupSelector(..., repoProviderHost("gitlab"),
    discovered.GroupFullPath, ...)`.
  - The existing `selection_observation_test.go` stays green unchanged.
- `observeSelection` (`selection_github.go:44-51`) gains `case "gitlabGroup"`.
  It still runs on shard 0 only, after discovery, before sharding
  (`selection_native.go:68-69`).
- The fatal-token checks at `selection_native.go:56` and `selection_cli.go:41`
  extend to `gitlabGroup`, and the git sync case at `selection_native.go:136`
  adds it.
- **Sharding.** `filterRepositoryIDsByShard` hashes the `gitlab/...` IDs exactly
  like other IDs. Every shard lists; only shard 0 observes.
- **Webhooks.**
  - The webhook selector never observes.
  - A GitLab trigger yields the same `gitlab/<path>` ID
    (`webhook_trigger_selector.go:306-307`), so a webhook refresh and a listing
    selection share one checkout and one scope.
  - The webhook path applies no group or rule check (`:299`). A webhook can
    therefore sync a project inside the group that the rules exclude; the next
    listing records it as `rule_excluded`, which is truthful. A project outside
    the group is never judged and reads `unknown`.
  - **Provider guard (credential safety).** `repositoryIDFromWebhookTrigger`
    returns a bare `org/repo` for a GitHub trigger (`:303-305`) and
    `bitbucket/org/repo` for a Bitbucket one, with no source-mode check. In
    `gitlabGroup` mode that ID is cloned from another host with the GitLab token
    in `config.GitToken`. PR #7821 (head f727f0080) does not stop it: its
    fail-closed rule (`tokenAuthProvider` returns `ok=false`) covers only paths
    outside `ReposDir` and the `.eshu-ref-worktrees` namespace. A webhook-derived
    path under `ReposDir` is a managed checkout, a bare `org/repo` resolves to
    provider `github`, and the header is scoped to github.com.
  - So in `gitlabGroup` mode the webhook selector drops every trigger whose
    provider is not `gitlab` before it builds a repository ID, marking it failed
    with the bounded reason `provider_not_selected` (a new constant beside
    `webhookTriggerFailureUnsupportedProvider`, `:37-39`). It reuses the split in
    `supportedWebhookRefreshTriggers` (`:260-275`) and the failure marking at
    `:135-140`. Other modes are unchanged.

## Telemetry

The counter gains `selector_kind="gitlab_group"`, the scope gauge (which has no
`selector_kind` label) is sampled for `gitlab_group` cycles, and a new
`eshu_dp_collector_repository_listing_requests_total` counter plus two log
lines cover the GitLab listing. The full contract and the docs to update are in
[the proof and rollout page](7765-gitlab-group-discovery-proof-and-rollout.md#telemetry).

## Risks, rejected alternatives, open questions

**Risks.**
- **A pending-deletion project may change path.** `active=false` returns
  "archived or pending-deletion projects" (Groups API), so they appear in an
  unfiltered listing. If deletion scheduling renames the path, a new scope
  would be cloned. Mitigation is an open question below.
- **Line cap.** `selection_discovery.go` lands near 466 lines. If it crosses
  500, the fallback is moving `reportRepositoryBasenameCollisions` (`:357-441`)
  into a leaf. Moving it to a new file in `git/` is not allowed.
- **Shard multiplication.** N shards × pages per cycle count against the
  600/min group-projects limit.

**Rejected alternatives.**
- First-segment partition with owner = top-level group: mass false
  `not_listed`.
- First-segment partition with owner = full path: no scopes ever judged.
- One parameterized SQL for all kinds: changes the `github_org` plan and text.
- `LIKE` prefix: `_` wildcard bug.
- Keyset pagination: undocumented for this endpoint.
- Reusing `gitlabciruntime.GitLabClient` for the listing. It is bound to the
  CI pipeline `TargetConfig`, makes a single request with no retry, reads
  `X-Total` (unusable past 10,000 records), returns untyped maps, and lives
  under the `cicdrun` family. The leaf keeps its own small client but **reuses
  the helpers**: `sdk.ParseRetryAfter` (seconds or HTTP-date, 0 when absent or
  past, `collector/sdk/http.go:111`) feeds the retry wait, which the leaf then
  clamps to [1s, 60s], and `sdk.ParseBaseURL` (`:88`) validates `apiBase`.
  `sdk` imports no Eshu package, so the leaf stays a leaf. `sdk.DoJSON` is not
  used because it hides response headers (`Link`) from the caller, and
  `sdk.ShouldRetryStatus` retries every 5xx where this design retries only 502,
  503 and 504. The `PRIVATE-TOKEN` header matches that client's.
- GraphQL `group.projects`: a second client stack for no gain.
- SSH clone plus a separate `ESHU_GITLAB_TOKEN`: two credentials and chart
  changes. Deferred.
- Renaming `github_repo_id`: churn with no reader that needs it.
- Adding `selector_kind` to the gauge: a contract change with no collision to
  fix.
- `min_access_level` by default: membership-dependent false departures.

**Open questions.**
- Handling of pending-deletion projects (`marked_for_deletion_on` or similar;
  field NOT_CHECKED). The proposal is to treat them as not listed.
- Self-managed API base paths beyond the host (#7764).
- Re-scoping on a host change (#7764).

**NOT_CHECKED.**
- Whether `simple=true` returns `archived` (not used), and whether GitLab's
  maximum-offset pagination limit applies to `/groups/:id/projects`.
- Whether omitting `archived` returns both archived and active projects. The
  Groups API docs leave that default unstated, and the design relies on it. A
  live check against a group with an archived project settles it; the
  `Archived` decode test cannot.
- Whether a renamed configured group path returns 404 or a redirect. Go's HTTP
  client follows redirects, so a redirect would resolve the new `full_path`
  silently instead of failing the cycle.
- Whether GitLab namespace routing is case-insensitive. The design compares
  lowercased paths and keeps GitLab's case in rule IDs.
- The auto-generated name of the `selector_kind` CHECK constraint (verify with
  `\d` before writing the migration), and whether an open PR claims migration
  169 (no fetched remote branch does).
- Whether PR #7821 changes before merge. The design reads branch
  `fix/7763-token-auth-host-scope` at `f727f0080`.
