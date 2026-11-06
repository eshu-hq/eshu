# Lockfile changes reopen security-alert scopes (#10061)

**A lockfile delta that adds a pin named by an active
`security_alert.repository_alert` fact for the generation's repository now
reopens that alert scope's `supply_chain_impact` holder, through the same
#9874 marker pass that already reopened advisory holders. This note records
the threshold stated before the probe, the EXPLAIN numbers, and the test
evidence.**

Design: issue #10061 (part of #9209, follow-up to #9874). All volumes below
are synthetic; no QA data was read.

## What changed

| Layer | Change |
| --- | --- |
| Reducer port | `FindPackageHolderCandidates` now takes `PackageHolderQuery{Ecosystems, RepositoryIDs}` instead of an ecosystem slice. The struct is declared in `manifest_trigger.go`; the `fanout.go` diff is the three signature/comment lines, so the file stays at 499 lines, inside the #9874 zone ruling allowance for that file (one port method, within 10 choice lines). |
| Reducer branch | `manifestChange` tracks the distinct repository ids the changed keys' dependency rows name (`manifest_change.go`), and the marker pass passes them as `RepositoryIDs`. Candidates from both arms go through the unchanged `matchPackageHolders` consumption-key join, so an alert naming an unchanged package of the same repository does not reopen. |
| Storage | `supply_chain_impact_package_holders.go` adds an alert arm beside the affected-package arm, in the same bounded read transaction: active-generation `security_alert.repository_alert` rows with `payload->>'repository_id' = ANY($1)`, the leading expression of `fact_records_security_alert_repository_lookup_idx` (migration 003). No migration, no new index. Alerts with `provider_state` in (`dismissed`, `auto_dismissed`, `fixed`) are excluded — the same set `securityAlertCanSeedImpact` refuses to seed a finding from. |

## Decision, threshold, deadline (stated before the probe)

- Alert-arm read: proceed if the median `EXPLAIN (ANALYZE, BUFFERS)`
  execution stays under 50 ms at 10x — the bound #9874 used for its holder
  read — and the `fact_records` side uses
  `fact_records_security_alert_repository_lookup_idx` at every scale (no
  sequential scan of `fact_records`).
- 1x basis: 5,000 active alert rows (1,000 alert scopes, one per repository,
  five alerts each), each with a superseded-generation copy, matching the
  5,000-row census ceiling #9874 used. 5x is 25,000 active, 10x is 50,000.

## Probe

Method: PostgreSQL 18.6 in a scratch container, schema from the live-test
bootstrap template, seeded by a throwaway probe test (not committed) that
inserted the alert scopes, generations, and facts with `generate_series`,
ran `ANALYZE`, and explained the exact alert-arm statement five times per
scale with a one-repository array. Payloads carry a 400-byte pad field. The
queried repository holds five active alerts (ten rows with the superseded
copies).

| Scale | Median execution | Runs (ms) | `fact_records` access |
| --- | --- | --- | --- |
| 1x (5,000 active + 5,000 superseded) | 0.54 ms | 0.445, 0.483, 0.537, 0.562, 0.608 | Index Scan on the lookup idx, 8 buffers, 10 rows in 0.043 ms |
| 5x (25,000 + 25,000) | 2.35 ms | 2.290, 2.310, 2.352, 2.518, 2.589 | Bitmap Index Scan on the lookup idx, 3 buffers, 10 rows in 0.017 ms |
| 10x (50,000 + 50,000) | 5.47 ms | 5.344, 5.406, 5.474, 5.640, 5.886 | Index Scan on the lookup idx, 9 buffers, 10 rows in 0.054 ms |

The bound holds at every scale (10x median 5.47 ms against a 50 ms bound,
and against the existing 10 s holder-read statement timeout). The
`fact_records` side is flat in the volume — the index returns the
repository's rows directly. What grows is the `ingestion_scopes` hash the
active-generation join builds (a sequential scan of the scopes table: 29,
143, 286 buffers at 1k, 5k, 10k alert scopes). That is the same join shape
the affected-package arm already runs on every marker pass with changed
packages, so this change adds one more statement of that shape, not a new
cost class. Planning time was 2.0–2.7 ms throughout.

## Tests

- `TestManifestTriggerReopensSecurityAlertHolderLive` (live, real queue):
  a lockfile delta generation that adds a lodash pin reopens the alert
  scope whose active repository alert names lodash (succeeded → pending).
  Four sibling alert scopes stay `succeeded`: an alert for another package
  of the same repository, an alert for lodash in another repository, a
  dismissed lodash alert, and a lodash alert present only in a superseded
  generation. RED before the fix: the lodash alert holder stayed
  `succeeded` (exit 1), all four negative scopes already passed.
- `TestManifestTriggerReopensSecurityAlertHolder` (unit): the marker pass
  reopens a purl-less alert candidate naming the changed package, does not
  reopen an alert candidate for the unchanged react pin, and asks the port
  with `RepositoryIDs: ["repository:r_checkout"]` beside the ecosystems.
- Unaffected and still green live: `TestFindPackageHolderCandidatesLive`,
  `TestFindPackageHolderCandidatesMatchesRawEcosystemSpellingsLive`,
  `TestManifestTriggerReopensAdvisoryHolderLive` (call sites updated to the
  query struct, assertions unchanged).

## Telemetry

Observability Evidence: no new instrument. Alert-scope reopens ride the
existing fan-out telemetry under `cause="manifest_dependency"`:
`eshu_dp_supply_chain_impact_fanout_total{cause,outcome}` counts each
reopen outcome, and the marker result's sub-signals and success log carry
the holder counts, exactly as advisory holders do. What separates an
alert-holder wave from an advisory one is the reopened holder's scope id on
the per-holder fan-out log line. A failed alert-arm read surfaces as the
existing `read_error` / `read_timeout` fan-out outcome and fails the pass
retryably, like the affected-package read.

## Not checked

- No QA readback: alert-scope counts, alert rows per repository, and the
  marker pass rate on QA were not measured; the 1x basis is the #9874
  census ceiling, not a QA count.
- The alert arm keys on the repository id the changed dependency rows
  carry. A generation whose dependency rows carry no repository id asks
  with an empty repository list and reopens no alert scope — the pre-#10061
  behavior for such rows.
- Removals of pins only a delta's unreadable base held stay invisible to
  the changed-key derivation (the #10059 known limit), so an alert whose
  pin was removed that way is still not reopened; that limit is unchanged.
