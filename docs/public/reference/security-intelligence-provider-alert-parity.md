# Security Intelligence Provider Alert Parity

## Provider Alert Parity Gate

Provider-hosted alert parity is a validation gate, not a source of public test
data. For supported hosts, private validation may compare Eshu findings against
provider alerts for the same repositories and package ecosystems.

`eshu-collector-security-alerts` is the hosted provider security-alert runtime.
It is claim-driven and supports GitHub Dependabot alerts in two scopes selected
by the target `scope` field (default `repository`):

- `repository`-scoped targets poll one repository
  (`GET /repos/{owner}/{repo}/dependabot/alerts`) behind explicit credentials
  and a `allowed_repositories` allowlist. Every repository must appear in the
  target's `allowed_repositories` list before the runtime issues an HTTP request.
- `org`-scoped targets poll one organization
  (`GET /orgs/{org}/dependabot/alerts`) and split the response into
  per-repository `security_alert.repository_alert` facts keyed on each alert's
  source repository, with the same per-repository fact shape so reducer
  reconciliation is unchanged. The org read is a bounded window, not a full
  scan; see [Org-scope window and per-repository fan-out](#org-scope-window-and-per-repository-fan-out).

Targets name a `token_env` rather than a token value. Any `api_base_url`
override must use HTTPS because the runtime sends the bearer token to that
endpoint. Both scopes share the same bounded cursor pagination, cross-host
next-link rejection, and rate-limit handling. Collection is bounded by
`repository_alert_limit` and `max_pages`, requests GitHub's open-alert view
directly, and surfaces `source_freshness=partial` plus a coverage summary when
the open-alert provider page cap is reached. A rate-limited first read for a
target is surfaced as a retryable workflow failure.

### Org-scope window and per-repository fan-out

GitHub returns an organization's open alerts newest first across every
repository and pages them by cursor only. An org read therefore covers at most
`max_pages` x 100 alerts for the whole organization before the
`allowed_repositories` filter runs. On a large organization a quiet allowlisted
repository can have few or none of its alerts inside that window, even with the
maximum of 100 pages.

When the org read hits `max_pages`, the runtime reads each allowlisted
repository of that organization through
`GET /repos/{owner}/{repo}/dependabot/alerts`, each read bounded by the same
`max_pages`, and merges the results into the same generation and `scope_id`:

- A repository whose own read completes replaces its org-window alerts and
  reports complete coverage. Facts keep one stable key whichever read produced
  them, so the org path and the per-repository path are idempotent.
- A repository whose own read still hits `max_pages` keeps
  `collection_incomplete_reasons=["provider_open_alert_page_limit_reached"]`
  and counts on `eshu_dp_security_alert_truncated_reads_total`.
- Reads run one at a time in allowlist order. The first rate-limited read stops
  the fan-out; that repository and every later one keep only their org-window
  facts with `org_repository_read_rate_limited`. The claim still commits, and
  the next scheduled claim reads again. Nothing retries inside the claim.
- A `not_found` read marks that repository `org_repository_read_failed` and the
  fan-out continues. A 403 with no rate-limit exhaustion signal (no `Retry-After`, and
  `X-RateLimit-Remaining` not 0) whose JSON body says
  `Dependabot alerts are disabled for this repository.` marks that repository
  `org_repository_alerts_disabled` and the fan-out continues too: the answer is
  specific to one repository. The match is on GitHub's message text, which the
  REST reference does not document, so a body that is not JSON, is cut off,
  or is worded differently falls back to the stop below. A repository-scope
  target that gets this 403 fails as terminal `not_found` (#8947), so
  readiness reports `terminal_fetch_failure:not_found`, not a credential
  failure; read `not_found` on `list_repo_alerts` as "the repository is missing
  or its Dependabot alerts are disabled". Any other failure (`auth_denied`
  without that message, including a plain 403, a 5xx or transport error, or a
  terminal 4xx) marks that
  repository `org_repository_read_failed` and stops the fan-out: later
  repositories keep their org-window facts with `org_repository_fanout_stopped`.
  GitHub warns that continuing to call while limited can get an integration
  banned, and the next read would use the same credential.
- At most 100 repositories are read per claim, and a repository that answers
  `alerts_disabled` counts toward that cap. Later allowlisted repositories
  keep their org-window facts with `org_repository_fanout_cap_reached`.
- **Where the next claim starts (#8946).** The walk stops at the first
  failing repository and labels the repositories behind it
  `org_repository_fanout_stopped`; the failing one is `org_repository_read_failed`.
  For an allowlist of at most 100 same-org repositories each claim starts at a
  cursor, a repository name kept per org target, and the start moves forward
  through the allowlist, to the repository after a failed read. The next claim
  therefore reads the repositories behind the failing one, wraps around, and
  retries the failing one last. A failed read at the last position closes the
  lap: the start returns to the top, which is where it stayed before #8946, and
  stays there until a claim reads every repository or that last repository
  leaves the allowlist. A claim that reads every repository resets the start to
  the top and ends the lap. A rate limit leaves the start where the claim
  began, because the limit is provider-wide and the repository is not at fault.
  With several persistent failures the start settles after at most one stored
  generation per failing repository plus one, and the committer then skips every
  unchanged generation. The settled read set is never smaller than the one
  start-at-top reads. A repository behind the settled start stays unread until
  the failing repository is fixed or removed; fix or remove every repository
  labeled `org_repository_read_failed`, in order. A cursor or lap naming a
  repository that left the allowlist is ignored, so removing a failing last
  entry resumes the forward walk.
- **A target over 100 same-org repositories is collected as shards** (#9019).
  The coordinator plans one work item per shard of the target, computed from
  the configuration alone: a binary trie over a hash of each normalized
  `owner/name` splits any group of more than 100 distinct same-org
  repositories, so every shard holds at most 100. A target of at most 100
  keeps its `scope_id` unchanged; a larger one is collected as
  `<scope_id>#shard-<bits>` scopes, each a full scope of its own with its own
  generations, read state, and fan-out cursor. Every repository is then read
  within one scan interval. Shard ids never carry a repository name, and a
  configured `scope_id` must not contain `#shard-` (validation refuses it).
  The split rule has no hysteresis: adding one repository splits at most one
  shard into two, and removing one can merge two back.
- **Shards no longer planned are retired.** After a split (the base
  `scope_id`) or a merge (both leaves), the coordinator plans one retirement
  work item for each scope of the target that still holds data and is no
  longer planned. The collector answers it, without a provider call, with an
  empty generation that it attests as a deliberate deletion (so the #7926
  empty-successor guard lets it through) and a read state of zero
  repositories. List, count, and inventory stop returning that scope's alerts
  once the projector activates it, and a repository removed from the allowlist
  leaves every read on the next scan. Up to 1,024 scopes retire per instance
  per scan. Until a re-created scope's first collection activates (the merged
  parent, or a shard name planned again), its active generation is still the
  earlier retirement, and an empty answer for its repositories reads as still
  collecting (`no_committed_generation`), never complete. A target removed
  from the configuration or re-scoped (switched to a repository target, or its
  `scope_id` renamed) leaves its scopes unretired for now (#9071), and an
  ordinary shrink of a scope's own allowlist can still be refused by the
  #7926 guard (#9072).
- **Scoped tokens.** A token granted an org target's base `scope_id` sees the
  alerts of all its shards: the alert reads match a row whose scope id is
  `<granted>#shard-<bits>`, exactly and only that. Repository-id grants work
  as before. The extra match costs about one more grant-array comparison per
  shard row a token is not granted (113 to 141 ms on 20,480 rows with 200
  grants, measured).
- **Upgrade order.** Upgrade the collector and the API before the
  coordinator; all three ship in one image set. A new coordinator with an old
  collector plans shard scopes the old collector does not know (they fail
  retryable until it upgrades). A new collector claimed a sharded target's base
  scope by an old coordinator, or by one that reads a different configuration
  during a rolling restart, fails the claim retryable and logs
  `security_alert.target_sharded_coordinator_skew` instead of collecting a
  partial allowlist; it clears once both run the same version and
  configuration. An old API reads
  only the base scope of a sharded target.
- **A shard at the depth cap.** More than 100 repositories whose hashes share
  their first 32 bits cannot split further. That shard reads the same first
  100 on every claim, the rest keep `org_repository_fanout_cap_reached`, and
  the collector logs one `security_alert.org_fanout_over_cap` warning for it at
  startup. The cap is per claim: a rotating window over a snapshot scope
  stored a generation per claim and was withdrawn.
- **Request budget.** One shard claim costs `max_pages` x (1 + shard size, at
  most 100) requests, so one scan of a target of N same-org repositories in K
  shards costs at most `max_pages` x (K + N), measured over HTTP for N = 250,
  K = 4 (254 requests at `max_pages` 1, 508 at 2). GitHub documents 5,000
  requests an hour for a personal access token and most App installations and
  15,000 for GitHub Enterprise Cloud. Set the instance's `scan_interval` to at
  least `max_pages` x (K + N) x 3600 / budget seconds: 73 seconds for N = 100
  (K = 1), and about 732 seconds for N = 1,000 (K is about 16), `max_pages`
  1, 5,000 an hour.
- **Where the cursor lives.** `fanout_start_repository`,
  `fanout_next_repository`, and `fanout_lap_repository` on the org read state
  row (see [Org read state](#org-read-state)) name the repository this
  generation's fan-out started at, the repository the next claim starts at, and
  the last-position repository that closed a lap; NULL means the top and no lap.
  A lap in progress is `fanout_next_repository IS NULL AND fanout_lap_repository
  IS NOT NULL` on the newest generation's row. They commit in the generation's
  own transaction, so they survive a restart and move only when a generation
  commits. A claim whose generation is rolled back, or skipped because nothing
  changed, leaves them where they were. The next claim reads the position of the
  newest pending or active generation of the target. A target with no
  generation, or whose newest generation was committed before the cursor
  existed, starts at the top. A failed read of the cursor does not start at the
  top: it fails the claim as a retryable storage failure that the claim record
  shows as `security alert fan-out cursor read from postgres failed` (operation
  `read_fanout_cursor`), so the operator looks at Postgres, not at GitHub.

One claim sends at most `max_pages` x (1 + min(allowlisted repositories in the
organization, 100)) alert-page requests, whichever repositories the cursor
selects. Under `github_app` auth each listing
call also mints one installation token. Allowlisted repositories owned by
another account are never read through an org target. If every allowlisted
repository belongs to another account, a truncated org read fans out nothing and
keeps the plain truncated single-read result and digest.

Facts from a per-repository read carry a lowercase `repository_name`, because
that endpoint returns no repository object; org-window facts keep the
provider's casing. `repository_id` is lowercase on both paths.

A repository that the fan-out could not read and that has no alert in the org
window emits no fact, so no fact carries its reason. The collector records it
in a table of its own instead (see [Org read state](#org-read-state)), and the
`eshu_dp_security_alert_org_fanout_repositories_total` outcome counter counts
it.

### Org read state

Every generation an `org` target commits also carries one small record, written
in the same transaction (#8943): whether the org stream stopped at `max_pages`,
how many allowlisted repositories of the organization the target covers, and
each repository the fan-out did not read in full with a reason. A repository
read in full is not listed. A claim whose read is unchanged commits no
generation and writes none; the active generation's record still applies. The record lives in
`security_alert_target_read_states` (one row per generation) and
`security_alert_unread_repositories` (one row per unread repository). Both
tables cascade from `scope_generations`, so generation retention removes them
with the generation; they do not depend on `workflow_work_items`. A
repository-scope target writes none, because its read is one repository and a
truncated read of it always holds alerts.

To list the targets whose latest read was cut short:

```sql
SELECT s.scope_id, s.org_read_truncated, s.repositories_total, s.unread_total, s.unread_reasons
FROM security_alert_target_read_states AS s
JOIN ingestion_scopes AS sc ON sc.active_generation_id = s.generation_id
WHERE s.unread_total > 0 OR s.org_read_truncated
ORDER BY s.unread_total DESC;
```

The read path consults the record of the target's active generation when an
answer is empty, and reports the repository it asked about:

| Why the collector did not read the repository in full | `coverage.reasons` |
| --- | --- |
| Past the 100-repository per-claim cap | `org_repository_fanout_cap_reached` |
| A rate limit stopped the fan-out before or at the repository | `org_repository_read_rate_limited` |
| Its own read failed (`not_found`, a 5xx, a terminal 4xx, or a transport error) | `org_repository_read_failed` |
| An earlier failure stopped the fan-out | `org_repository_fanout_stopped` |
| Dependabot alerts are disabled for it | `org_repository_alerts_disabled` |
| Its own read stopped at `max_pages` | `provider_open_alert_page_limit_reached` |
| A reason a newer collector build stored that this build does not know | `org_repository_read_incomplete` |

For each of them the answer is `coverage.state=target_incomplete`,
`truth.level=derived`, and `truth.freshness.state=fresh`: the facts it rests
on are current, but the collector never read that repository in full. The
answer carries no `collector_readiness` and no freshness cause. A repository
the collector read in full still reads `complete`, even when the org stream
truncated and other repositories were left unread. When several targets cover a
repository, the healthiest one decides, so a repository-scope target that read
it in full wins. A terminal fetch failure on the covering target outranks the
unread reason. A read with no repository anchor reports `target_incomplete` when
any target left repositories unread, with each distinct reason. A generation
committed before this record existed has none, and its answer reads as it did
before; each org target commits one extra generation after the upgrade, which
writes its first record.

The read costs one more lookup per covering target on the same empty-answer
read: the target's active generation, its read-state row, and the requested
repositories' rows by primary key (about 0.5 ms for 50 targets with 2,000
unread repositories per generation; see
`docs/internal/evidence/8943-security-alert-org-read-state.md`).

Prefer `repository`-scoped targets when the allowlist is a small part of a
large organization. They cost the sum of `ceil(open alerts / 100)` requests
across the allowlisted repositories, while an org target whose window
truncates pays for the org window and then the same per-repository reads. Use
an `org` target when the organization's whole open-alert stream fits inside
`max_pages` x 100 alerts.

`security_alert.repository_alert` facts preserve repository-scoped provider
alert state from GitHub Dependabot. The runtime emits only that source fact
kind; provider alerts do not become canonical Eshu impact findings by
themselves. The `security_alert_reconciliation` reducer writes comparison rows
with provider state and Eshu impact state as separate fields:

- `matched` when the alert joins to owned dependency evidence and an Eshu
  impact finding for the same package/advisory.
- `unmatched` when the dependency is owned but no Eshu impact finding exists.
- `stale` when newer owned dependency evidence no longer matches the alert's
  manifest path.
- `dismissed` or `fixed` when the provider reports that state.
- `provider_only` when Eshu has no owned dependency evidence for the alert.
- `unsupported` when the provider alert names an ecosystem Eshu cannot match
  with the current impact matcher.
- `ambiguous` when multiple owned dependency evidence rows could match and
  Eshu refuses to guess.

Provider alert reconciliation reads require a repository, provider, package,
CVE, or GHSA anchor. Provider state and reconciliation status only filter an
anchored page; they are not standalone scopes.
List and count responses include a `coverage` object. `state=target_incomplete`
means at least one returned or counted reconciliation came from a truncated
open-alert provider read, so callers must not treat the count as complete.

### Empty answers

A zero-row answer is only as good as the collection behind it. When a list,
count, or first inventory page comes back empty, the API reads the
security-alert collector's state before it answers (#8803). Pages with rows
skip this read, because the rows already prove the collector ran.

| Collector state | `coverage.state` | `coverage.reasons` | `truth.level` | `truth.freshness.state` |
| --- | --- | --- | --- | --- |
| No enabled `security_alert` instance | `not_configured` | `collector_not_configured` | `fallback` | `unavailable` |
| No enabled target covers the repository | `not_configured` | `repository_outside_target_set` | `fallback` | `unavailable` |
| Covering target has no projected generation yet, or its latest projected generation did not collect the repository | `target_incomplete` | `no_committed_generation` | `derived` | `building` |
| Latest fetch failed terminally, nothing projected before | `target_incomplete` | `terminal_fetch_failure:<class>` | `fallback` | `unavailable` |
| Latest fetch failed terminally after an earlier projected generation | `target_incomplete` | `terminal_fetch_failure:<class>` | `derived` | `stale` |
| Projected generation, no later terminal failure, but the collector did not read the repository in full | `target_incomplete` | one of the `org_repository_*` reasons or `provider_open_alert_page_limit_reached` | `derived` | `fresh` |
| Projected generation and no later terminal failure | `complete` | (absent) | `exact` | `fresh` |
| Scoped token with no grants (no collector read) | `not_configured` | `no_authorized_repositories` | `fallback` | `unavailable` |

`repository_outside_target_set` also covers a repository Eshu cannot map to a
provider slug: one with no GitHub repository slug or remote URL and no provider
facts yet. Nothing proves such a repository is outside every target, so the
answer never reads `complete`; only the reason is broader than its name.

Every non-complete row classified from a successful collector-state read sets
`truth.freshness.cause=missing_collector_completion`, whose next check is
`list_collectors`, except the unread-repository row (see [Org read state](#org-read-state)):
the collector ran healthy and the facts are current, so that row is `derived` and
`fresh` with no cause. Key on `coverage.state` and `truth.level`, not on freshness
alone. `<class>` is the class the claim runner stored on the
terminal work item: `auth_denied`, `not_found`, `rate_limited`, or `terminal`
from the provider; `retryable` when the retry budget ran out on a transient
provider failure; `attempt_budget_exhausted` when the budget ran out on a cause
with no class; `identity_mismatch`; `collect_failure` or `commit_failure` for a
non-retryable collect or commit error; and `unclassified` for anything else.
The no-enabled-instance row and the healthy row also carry
`collector_readiness` (`not_configured` or `ready_zero_results`). The
outside-target row and the still-collecting and terminal-failure
(`target_incomplete`) rows carry none: an outside-target answer has an enabled
instance, so the envelope's `not_configured` would be false, and
`coverage.reasons` already says why the answer is not complete. The failed
state read is also `target_incomplete` but carries `readiness_unavailable`,
described below. These fields and the `coverage` object on an empty
inventory page are additive.

A scoped token whose grants authorize no repository is answered without
reading collector state, so its row carries no `collector_readiness` and no
freshness cause: it says nothing about collector health, only that no target
covers a scope the caller may read. The collector-state read runs under
a 1 s deadline. If it fails or times out, the page is still served, never as a
`5xx`, with reason `collector_state_unavailable`, `readiness_unavailable`,
`fallback` truth, and `unavailable` freshness. The stage log names why with
`error_class`: `deadline_exceeded`, `bound_exceeded`, or `query_failed` at
WARN, or `canceled` at INFO when the caller walked away (that answer is not
counted).

The read costs nothing on a page with rows. On an empty answer it scans each
covering target's whole work-item history through the collector/scope index,
about 1 ms per target with 2,000 work items on a vacuumed table and more when
recent rows still need heap reads. A read with no repository anchor checks every target; a scoped unanchored read checks only the targets covering the caller's granted repositories.
Work-item history is not pruned, so the cost grows as the collector runs.

A repository counts as covered when a repository-scope target names it, or when
an organization-scope target's `allowed_repositories` lists it under the same
owner. When several targets cover it, the healthiest one decides. A read with
no repository anchor needs every configured target to be healthy, and its
reasons list each distinct failing state. A scoped unanchored read narrows to
the caller's grants: it needs only the targets covering the caller's granted
repositories to be healthy. That makes an unscoped unanchored `complete`
a statement about configured-target health only: it says nothing about whether
any particular repository is inside a target, while a scoped token narrows
classification to the caller's grants. Anchor by `repository_id` for repository
coverage.

An empty security-alert list, count, or first inventory page reports
`coverage.state=complete` with exact, fresh truth only when the security-alert
collector has a projected generation and no later terminal fetch failure for a
target that covers the requested repository (or, unanchored, for every
configured target; for a scoped token, only the targets covering the caller's
grants). It does not cover a pending or dead-lettered reducer reconciliation
on an unanchored read, and a caller
with no grants gets `not_configured` with reason `no_authorized_repositories`.

Coverage reflects each target's latest projected generation and the
repositories that generation collected (#8838). Each generation records its
target set in `scope_generations.payload`: `security_alert_target_set` (the
repository of a repository target, or the organization's allowlisted
repositories of an org target, sorted) and `security_alert_target_set_hash`. A
repository added to an allowlist, or a `scope_id` reused for another
repository, reads `no_committed_generation` until a generation that collected
it projects. The set's hash is part of the freshness hint, so an allowlist
change always commits a new generation, even when the provider's alerts did
not change; each target commits one extra generation after the upgrade. A
generation committed before the set was recorded keeps the earlier answer. A
zero-row answer is clean only when coverage.state is complete and the
reducer's security_alert_reconciliation domain has no pending or dead-lettered
work for the scope (`get_freshness_causality`). A later success clears a terminal
failure: freshness flips back to `fresh` once the newest work item completes.

The answer never names a target, another repository, an allowlist, or a failure
message. A scoped token reaches the collector read only for a repository inside
its grants; a selector outside them answers `404` first.

An empty answer for a repository the org fan-out could not read no longer reads
`complete`: the org read state above names it. What the record cannot tell is a
repository that is not in the target's `allowed_repositories`; that reads
`repository_outside_target_set`.

Eshu should match provider alert counts when it has equivalent owned target
evidence and advisory data. Eshu may exceed provider alert output when it can
add code-to-cloud context, image/runtime impact, or additional advisory sources.
Any mismatch must classify whether the cause is missing target collection,
missing advisory ingestion, version-range matching, unsupported ecosystem,
provider-only behavior, or an Eshu reducer bug.

Public-safe readback for scoped npm alerts: synthetic `@scope/provider-owned`
fixtures prove a provider-only alert stays `provider_only` when no owned
dependency evidence exists, then promotes to `matched` reconciliation and a
reducer-owned `affected_exact` impact finding when Eshu has lockfile-proven
transitive dependency evidence for the exact scoped package name.

Operator-local `eshu vuln-scan provider-parity` runs this comparison across a
local repository allowlist and emits only aggregate-safe output. The public
summary includes repository count, provider alert count, Eshu finding count,
approved mismatch class counts, truncation, readiness state, and freshness
state. Row-level provider alerts, repository names, package names, advisory ids,
alert URLs, and raw Eshu finding rows stay in the operator-local evidence set.
If a row lands in `unclassified`, open a follow-up issue with private evidence
kept outside the public repo before claiming parity for that validation set.

No-Regression Evidence: `go test ./internal/collector/securityalerts ./internal/collector/securityalerts/alertruntime -count=1`
proves the provider client requests the `state=open` view, preserves cursor
pagination bounds, and marks truncated open-alert reads as partial source
freshness. `go test ./internal/reducer -run
'TestBuildSecurityAlertReconciliations|TestSecurityAlertReconciliationWriterUsesProviderAlertScopeForPackageTriggeredRepair'
-count=1` proves source freshness and collection coverage survive reducer
reconciliation payload publication. `go test ./internal/query
./internal/query/supply/chain/alerts -run
'TestSupplyChainListSecurityAlertReconciliations|TestDecodeSecurityAlertReconciliationRowPreservesProviderCoverage|TestSecurityAlertReconciliationAggregate'
-count=1` proves API/MCP-backed list and count responses expose partial
coverage without unbounded page reads.

No-Observability-Change: the fix reuses existing security-alert provider
request counters, fact-emitted counters, fetch-duration histograms,
`security_alert.observe`, `security_alert.fetch`, and the API/MCP response
envelope. No repository name, package name, alert URL, token environment name,
or token value is added to metric labels, status errors, or public docs.

Validation logs may record aggregate counts and mismatch classes. They must not
commit private repository names, package names, alert URLs, or copied provider
payloads to the public repo. Runtime metrics and spans use bounded provider,
status-class, and fact-kind labels; credentials are resolved from environment
variables and must not appear in facts, logs, metric labels, status errors, or
checked-in examples.
