# Arbiter ruling 2026-11-09: #10037 remainder (S4/S5/S6/S7)

Provenance (record-keeping, not part of the ruling): the text under
"Ruling (verbatim)" is the full result of arbiter-eshu (muse-wonder-full-1.0),
subagent 01a1c046-d824-79a0-8179-3c29e7976f91, dated 2026-11-09, recorded by the
coordinator per the file's own Decision 1 (arbiters do not edit; coordinators
record). The ruling was also posted on eshu-hq/eshu#10037 as comment
6740099274. Scope: S4/S5/S6/S7 remainder disposition only.

Trackers created under this ruling on 2026-11-09:
- S4 (DEFER-transfer, satellite lane):
  eshu-hq/eshu-collector-vulnerability-intelligence#75
- S5 (OPEN-core, worked in-goal): eshu-hq/eshu#10464 (Part of #9959)
- S6/S7 (DEFER-owner, recovery trigger in issue): eshu-hq/eshu#10465
  (Part of #9959)

## Ruling (verbatim)

RULING: Do NOT close #10037 unconditionally — CLOSE only after S4/S5/S6/S7 are transferred to named trackers below; until then #10037 stays OPEN and GHSA stays off and not activatable.

Arbiter: `arbiter-eshu` (`muse-wonder-full-1.0`). Issue: eshu-hq/eshu#10037.
Skills applied: `eshu-issue-driver`.
Record path for the coordinator: `docs/internal/arbiter/10037-remainder-ruling-2026-11-09.md`.
I do not write that file. See Decision 1.

## Evidence I verified myself

- Landed: #10444 MERGED 2026-11-09T02:06Z and #10461 MERGED
  2026-11-09T11:51Z via `gh pr view`.
  Origin/main `8be284148c` via `git log origin/main`.
- Design acceptance on origin/main via
  `git show origin/main:docs/internal/design/10037-ghsa-window-scope-decision.md`:
  S1/S2/S3 boxes checked.
  S4 `ghsaNextLink` and S7 not-activatable boxes unchecked.
- Proof files exist on origin/main via `git ls-tree -r origin/main`.
  Examples: migration 286, `newest_copy_10042_test.go`,
  `source_state_ghsa_window_test.go`.
- Issue comments via `gh api .../issues/10037/comments`.
  The 2026-11-09T02:12:21Z comment lists remaining criteria.
  The 2026-11-09T10:44:40Z comment says S6/S7 stay deferred.
  The 2026-11-08 S6 request asks for owner, window, and secret name only.
  No later comment supplies those three acts.
- No tracker exists yet.
  Eshu open search returned only #10037, #10401, #9959, and #9209.
  Satellite `eshu-hq/eshu-collector-vulnerability-intelligence` open issues are
  only #54 and #33 via `gh issue list`. Neither is S4.
- No ruling file exists yet.
  `docs/internal/arbiter/` holds only the #9532 and #9740 files.
- Brief premise falsified in one load-bearing part: S5 is not landed.
  It is eshu-repo work, not satellite or owner work.
  See Decision 3.

## Numbered decisions

1. **Recording — coordinator writes, arbiter does not.**
   The convention in
   [9740-retest-ruling-2026-11-08.md](/home/ubuntu/personal-repos/eshu/docs/internal/arbiter/9740-retest-ruling-2026-11-08.md:3)
   is clear.
   That file records an arbiter comment retrieved via the GitHub API.
   Its line 69 states the arbiter does not edit; the coordinator records.
   So the coordinator must record this ruling verbatim at
   `docs/internal/arbiter/10037-remainder-ruling-2026-11-09.md`.
   Add a provenance header in the #9740 style.
   Post the ruling on #10037 first if the coordinator wants comment provenance.
   Do not treat this message alone as the recorded file.

2. **S4 — DEFER by transfer to the satellite. Activation-blocking.**
   Track in a new issue in
   `eshu-hq/eshu-collector-vulnerability-intelligence`.
   Do not track it only in eshu-hq/eshu.
   Code `ghsaNextLink` appears in this repo only as design citations in
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:250).
   The satellite issue must cover full S4, not only the Link rule.
   Full S4 is defined at
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:314):
   pin factschema, resolve window scope, `covered_through` with `Date` clamp,
   prefix cut, malformed `Link`, and release.
   The Link rule is §9 at
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:245):
   malformed fails retryable as `ghsa_link_malformed` with no facts.
   Reason: under cursor windows, a false last page advances past unfetched
   records forever. That is an accuracy loss, not a cosmetic P3.
   Acceptance proof in the satellite: each coverage-table row, seeded
   `rel='next'`, snapshot not complete, no facts published.
   Link the satellite issue from #10037 and #9959.
   Use only existing labels in that repo. Do not invent one.

3. **S5 — NOT deferred. Keep in eshu core. Brief omission corrected.**
   S5 is core lane-3 work at
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:315):
   readiness rolling leg, `fetchConfirmedSources`, read-model newest copy.
   On origin/main, `fetchConfirmedSources` is `{"osv","nvd","glad"}` with no
   `ghsa` in
   [readiness_source_age.go](/home/ubuntu/personal-repos/eshu/go/internal/query/querycontract/packet/readiness_source_age.go:52).
   No `readiness_postgres_query_ghsa_window.go` exists on origin/main.
   Only F1 target admission exists, via `readiness_ghsa_target_live_test.go`.
   Therefore "no eshu-repo action possible" is false.
   Either land S5 or file a dedicated core issue under #9959 before closing
   #10037. Labels must be existing ones, for example
   `collector:vulnerability` and `type:task`.
   Until then, the Activation criterion "S1 to S5 merged" at
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:355)
   is false.

4. **S6 — DEFER to owner lane under #9959.**
   S6 needs three owner acts already requested on #10037 on 2026-11-08:
   named run owner, 72-hour QA window, and secret NAME only, never the value.
   No agent can supply those. No agent may invent a token, owner, or window.
   Track the run under #9959, which is the GHSA source issue and part of #9209.
   Prefer one dedicated QA-run issue in eshu-hq/eshu, part of #9959.
   Its acceptance is the 72-hour criterion at
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:361):
   both types, lag bound, no `no_progress`, request ceiling, no secret leak.
   The #10444 "live QA cursor proof" is not S6. Do not count it as S6.
   Recovery trigger: owner posts owner + window + secret name.
   Then schedule the run. No other approval is needed.

5. **S7 — DEFER, gated, not owner-discretionary.**
   Track under #9959 as the activation step.
   Execute only as paired PRs: satellite README plus core docs together, per
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:317).
   Core still carries the note in
   [security-intelligence-source-coverage.md](/home/ubuntu/personal-repos/eshu/docs/public/reference/security-intelligence-source-coverage.md:183).
   That page says GHSA is not activatable until #10037 lands.
   Removing any note before all Activation criteria hold, with evidence in the
   S7 PR per
   [10037-ghsa-window-scope-decision.md](/home/ubuntu/personal-repos/eshu/docs/internal/design/10037-ghsa-window-scope-decision.md:353),
   would be a false activatable claim.
   Criteria include S1–S5, satellite pin in `eshu-deploy`, three-window
   fixture, 10-then-6 run, prefix proof, S6, malware volume, late records,
   and catalog probe. An owner preference alone lifts none of them.

6. **#10037 — conditional CLOSE only. #9959 is the parent. #9209 via #9959.**
   Verdict per item: S4 DEFER-transfer, S5 OPEN-core, S6 DEFER-owner,
   S7 DEFER-gated, #10037 CLOSE only after transfer proof below.
   The issue body opens `Refs #9959`, and #9959 states `Part of #9209`.
   So activation remainder belongs to #9959 directly, not to #9209 directly.
   #9209 retains it by rollup through #9959. Do not file duplicates on both.
   Closing #10037 today, with its own S4/S7 acceptance lines unchecked and no
   tracker, would delete the only watch. That trades away accuracy.

## Required proof before the coordinator closes #10037

Post all of this on #10037 in the closing comment:

1. Satellite S4 issue URL, plus `gh issue view` state showing OPEN.
2. S5 disposition: merged PR link, or new core issue URL under #9959.
3. S6/S7 tracker under #9959: issue URL, or explicit #9959 comment carrying
   both with `DEFER: #N docs/internal/arbiter/10037-remainder-ruling-2026-11-09.md`
   per `eshu-issue-driver`. A created issue also goes in the goal ledger.
4. Pointer repair: design-doc Acceptance section and
   `security-intelligence-source-coverage.md` no longer say activation waits
   only on "#10037 lands." They must point to the new trackers.
   A dangling pointer after close is a defect.
5. Landed proof already on record: #10444, #10461 and #10130 links.
6. Explicit sentence: "GHSA remains off by default and not activatable."
   Close reason must claim only scope design plus core S1–S3 complete.
   It must not imply GHSA is activatable.

If any item is missing, keep #10037 OPEN.

## What I will not do, and why

- I will not write the ruling file, edit code, or close issues myself.
  Arbiter records via the coordinator, per Decision 1.
- I will not approve an unconditional close that erases S4/S6/S7.
  No tracker exists today, verified above.
- I will not shrink S4 to only `ghsaNextLink`. Full S4 includes release work.
- I will not accept S6 as done from cursor proof or from agent scheduling.
  Credentials and QA ownership are owner acts.
- I will not authorize dropping the not-activatable note early.
  That would publish a false production-truth claim.
- I will not invent satellite label names, issue numbers, or epic links.

## What stays unverified

- Satellite `ghsaNextLink` current implementation. NOT_CHECKED.
  I listed satellite issues only. I did not read satellite code.
- Satellite factschema pin and `eshu-deploy` pin state. NOT_CHECKED.
- S5 read-model SQL half, such as catalog `DISTINCT ON`. NOT_CHECKED.
  I proved only missing `ghsa` in `fetchConfirmedSources` and no window file.
  That is enough to prove S5 incomplete, not to scope its full remainder.
- QA token existence in any secret store. NOT_CHECKED, by design.
  Only the secret name may ever be posted.
- Local checkout note: this worktree HEAD `fb80c639` lags origin/main
  `8be284148c`. All landed-state claims above use `origin/main` reads.
