# Rollback And Removal Readiness (`#8879`)

Refs `#8879`. Part of `#6707`.

Status: **READINESS ONLY**. No live rehearsal, no deploy, no pin change,
no removal. The live rollback rehearsal would re-sync ops-qa through ArgoCD
and stays an owner/coordinator act. This record maps the acceptance criteria,
proves the rollback prerequisites read-only, and hands the first removal PR
a ready checklist.

## 1. Acceptance criteria vs `origin/main`

| # | Criterion | State | Evidence |
| --- | --- | --- | --- |
| 1 | Retained Git/parser boundary, intake-to-core contracts, durable-state ownership | PROVEN (prep) | `EXTRACTION_PREP.md` §2/§5/§6 via `#9351`/`#9360` |
| 2 | Six-capability inventory, dormant hosted activation | PROVEN (prep) | `EXTRACTION_PREP.md` §4; dormancy re-census at `5766b3592d` on `#9382` |
| 3 | Named owners, production target, config source | PARTIAL | Owner `@linuxdynasty` confirmed; ops-qa declared the production caller env (owner quote on `#9382`, 2026-11-02); public-contract/release/deployment owners and gate-1 satisfaction still open |
| 4 | Census record | PROVEN (prep) | `EXTRACTION_PREP.md` via `#9351`/`#9360` |
| 5 | Behavior/contract parity, representative + hostile | PARTIAL | Gate-2 sandbox 22/22 + corpus work on `#9382`/`#9728`/`#9729`; residuals open: `#10071` (proof-image re-pin + CI drift), `#10072` (amd64 digest null), `#10073` (WER/noisy-scan eval) |
| 6 | Reducer/graph/API/MCP truth on extracted path | PARTIAL | QA deploy truth verified live on `#9382`; satellite extracted path does not exist yet (`#10074` open) |
| 7 | Concurrency (retry, duplicates, ordering, cancellation, revoked authority, stale claims, partial failures) | OPEN | No bounded proof recorded for this lane |
| 8 | Bounded resources + no regression with signals | PARTIAL | Sandbox peak RSS ~30.7 MB on `#9382`; caller-lane perf gate PASS at HEAD (`#10051`); deploy-time health/MCP/queue evidence on `#9382` |
| 9 | Credentials, provenance, vuln response, version pairs | PARTIAL | Image cosign+provenance+SBOM verified (`#9382`); oldest/newest core/component pairs not recorded |
| 10 | Independent build/release/deploy on public contracts | PARTIAL | In-tree image `b41a05ba` published and deployed to ops-qa (`#534`/`#535`); independent satellite build/release/deploy frozen until both gates record (`#9382` freeze) |
| 11 | Rollback rehearsal | OPEN (ready, see §2-§3) | Disposition DEFERRED to first removal PR (`#9383`, arbiter 2026-10-28); prerequisites proven below, live rehearsal not executed |
| 12 | Independent review, local proof, exact-head CI; removal after acceptance | OPEN | Caller/gate-2 PRs merged with verdicts; production acceptance not recorded; no removal PR opened |

## 2. Rollback dry-run (read-only, 2026-11-05)

Target state: ops-qa running `b41a05ba`
(`sha256:18b9de74e86d6e8faf63f19dbefa878077cd36854541b463fcda8399e0ff911f`,
IaC `ab0438f`, `#534` + effective pin `#535`).

- Prior pins recorded in IaC history, no cluster read needed: immediate
  revert `sha-a854ed2a42...@sha256:112fc7a...` and last-known-good running
  `sha-e3d81617...@sha256:3a07c59b...` (both quoted in `ab0438f`).
- Migrations 270-277 verified additive in-tree: `CREATE TABLE/INDEX IF NOT
  EXISTS`, `CREATE OR REPLACE FUNCTION/TRIGGER`, `CREATE INDEX
  CONCURRENTLY` only. The one `drop` match in the range is a code comment
  (`272_...:140`, "sends the next tick down the full path"). No
  down-migration exists, so a pin revert needs no schema downgrade —
  consistent with the owner ban on downgrade rehearsals (`#9383`,
  2026-10-28) and the IaC commit note.
- Durable state is core-owned, not image-owned: Postgres owns queue,
  content, status, recovery, generation state; Neo4j owns projected truth
  (`EXTRACTION_PREP.md` §5). A pin revert preserves the 224 pre-existing
  dead-letter items (oldest 10-19, newest activity pre-deploy per `#9382`;
  triage owned by `#10076`).
- Activation posture unchanged by rollback: `go/cmd/collector-document-import`
  (`ESHU_DOC_IMPORT_ENABLED`, `config.go:19`, disabled by default, exits
  quietly) and `go/cmd/collector-media-import`
  (`ESHU_MEDIA_IMPORT_ENABLED`, `config.go:20`, QA-tier-only). Neither is
  enabled in ops-qa; a revert cannot silently enable or disable a caller.
- `b41a05ba` is an ancestor of this worktree HEAD (`0650a6a4d2`), so the
  readiness statements above describe the deployed content plus later main.

NOT_CHECKED: GHCR resolvability of the two prior digests from a clean pull
(no registry read attempted); ArgoCD sync RTO for a revert (needs the live
rehearsal); native-amd64 determinism (`#10072` open).

## 3. Live rehearsal procedure (owner-executed, not this lane)

1. Open the rehearsal on `#8879` citing this record and the `#9383`
   disposition for this boundary.
2. Revert the ops-qa file pin to the recorded prior digest (no schema
   change per §2), ArgoCD sync with prune, owner-triggered.
3. Verify the `#9382` evidence shape: all image deployments on the revert
   digest, pods ready with 0 restarts, API healthz ok, MCP probe ok,
   materialization queue all zero, main queue failed 0, dead letters still
   224 pre-existing, bootstrap index count unchanged, indexes valid.
4. Record wall-clock RTO (sync start to healthy) on `#8879`.
5. Roll forward to the current pin by the same path and re-verify §3 shape.
   Never downgrade schema; if a future migration is not additive, stop and
   take the boundary to arbiter-eshu before proceeding.

## 4. Removal readiness checklist (BLOCKED until production acceptance)

- [ ] Production acceptance recorded on `#8879` (gate-1 satisfaction items
      i-v from `#9382`, 2026-11-02, plus gate-2 with the real engine under
      the caller profile incl. native amd64).
- [ ] Residuals closed or explicitly deferred: `#10071`, `#10072`,
      `#10073`, `#10074`, `#10075`, `#10076`.
- [ ] Rollback disposition chosen at the first removal PR (rehearsal per §3
      or fresh quoted owner waiver + revival follow-up), per `#9383`.
- [ ] Removal scope follows runbook §7 ownership rule 5: delete
      `go/cmd/collector-document-import/`, `go/cmd/collector-media-import/`
      and the six capability packages only where no live core code imports
      them (whole-tree reference census, zero dead files); keep every
      package still imported by live core code.
- [ ] Retired-coverage honesty note in the removal PR body (runbook §9):
      name the retired suites and the accepted gap.
- [ ] Registries, contracts, specs, docs, Helm comments, telemetry rows,
      and skill baselines repointed in the same PR.

## 5. Production cutover decision path (arbiter input, not a decision)

Raw observations for arbiter-eshu:

- There is no satellite to cut over: `eshu-hq/eshu-ingester` stands at
  placeholder plus frozen lane work; the freeze (no satellite/deploy/
  stage/removal/IaC-pin until both gates record on `#9382`) holds.
- The deployed production path is the in-tree image on ops-qa, the
  owner-declared production caller env. Cutover therefore means enabling
  the disabled-by-default caller path on QA-tier after the gates, not a
  satellite traffic switch.
- Gate-1 satisfaction checklist (`#9382`, 2026-11-02) is the decision gate;
  gate-2 must be re-proven with the real engine under the caller profile
  incl. native amd64. Residuals `#10071`-`#10073` are the open proof items.
- Question: does enabling the in-tree caller on ops-qa after gate-1+gate-2
  record constitute the production cutover for criterion 10/12, with the
  satellite extraction following as a second cutover — or must the
  satellite exist before any enablement? Either answer keeps this lane
  read-only until the gates record.
