# workflow.v1 plane declaration for `#9376` (`#8877` declaration lane)

Status: **DRAFT DECLARATION — NOT A CUTOVER CLAIM**. No authority moves on
this file. The in-tree coordinator owns claims, leases, and scheduler state
until a standing cutover claim; the satellite owns nothing. Removal stays HELD
per `REMOVAL_PREP.md` §7.

Assembled at `origin/main` `15caae83c5` (2026-11-01) from read-only
evidence. Every scored item below names its proof or its gap. Gaps stay
gaps: acceptance is contingent on the spec match, and this draft does not
tick boxes it cannot prove.

Refresh 2026-11-04: this lane re-read only the digest record (§2) against
the 2026-11-04 live cluster state handed off to it. The doc was assembled
at `15caae83c5` (2026-11-01); this worktree sits at `8bcf063b64`
(2026-11-04). Sections without a 2026-11-04 date still cite the
2026-11-01 record.

Evidence vocabulary (the arbiter ruling on `#8877`, comment `6475436907`,
requires "absent on main" wherever a capability is not on main). Each claim
carries one of three tags and a `file:line` on the `origin/main` named above
(links are full-SHA permalinks to `15caae83c5`; the doc-citations gate rejects
raw `.go:N` pointers):

- **PRESENT-on-main**: the code is on `origin/main` and the production path
  calls it.
- **PROVEN-in-harness**: a test or parity harness on `origin/main` exercises
  it, but no production path calls it, or it lives in another repo.
- **ABSENT-on-main**: no code on `origin/main`. Say so; do not imply it.

## 1. Protocol major 1 + 20-route table

Protocol `workflow.v1`, major pinned to `"1"` on every client route through
the shared `(*ClaimPlaneClient).post` path, the sole
`Workflow-Protocol-Version` setter
([`claim_plane_client.go:158`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/workflow/claim_plane_client.go#L158)). Proof (PROVEN-in-harness;
the client is not wired into a production binary):
`go/internal/workflow/protocol_version_test.go`,
`TestClaimPlaneClientPinsProtocolMajorOne` — all 15 exercised routes
observed with major `"1"`; a reflect pin fails the build if any exported
`ClaimPlaneClient` method leaves the list.
`TestClaimPlaneClientVersionMismatchIsInvalidNotRejected` proves a 409
`version_mismatch` maps to invalid, never to a fence rejection
(`ErrClaimRejected`).

15 called routes (in-tree client, all pinned): `ClaimNextEligible`,
`HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`,
`FailClaimTerminal`, `PinClaimForCommit` (`ValidateAndPinClaimForCommit`),
`CheckClaimPacing`, `ListCollectorInstances`, `CreateRun`,
`EnqueueWorkItems`, `ReconcileCollectorInstances`,
`UpsertCompletenessStates`, `ReconcileWorkflowRuns`, `ReapExpiredClaims`.

5 uncalled routes (no in-tree client; the pin lives in `eshu-workflow`):
read plane `GetRunStatus`, `ListRuns`, `GetRunCompleteness`,
`QueueDepthSnapshot`; report plane `ReportRunProgress`. The satellite
`drive/8875-five-route-pin` work merged as `eshu-workflow#17`
(`5be1aaa`); this lane did not re-run it.

## 2. Tag + deployed digest + dark/authoritative (digest record refreshed 2026-11-04; 2026-11-01 history kept inline)

- Tag: `eshu-workflow` `v0.1.0` EXISTS at
  `09e23b93ad53c3c0acffd1c518f0439e7e0407fa` (verified 2026-11-01, read-only
  `git ls-remote --tags`). It is no longer the `main` head. No `v0.1.0` tag
  exists on `eshu-hq/eshu` (only `v0.0.x` + `sdk/go/*` tags) — the
  declaration tag is satellite-scoped by design.
- Tag `v0.1.1` EXISTS at `8311ed699c25201aba8d79c7a0990eaf00b42119` per
  read-only `git ls-remote --tags` (2026-11-04); it is a lightweight tag
  pointing straight at the 2026-11-01 commit whose `server/server.go`
  still serves only `workflow.v1` routes (read-only raw fetch 2026-11-04),
  so the §3 oldest/newest=v1 claim still holds.
- Satellite `main` is `af3311e0553908597271707781a29823678b2070`
  (2026-11-01, read-only `gh api` 2026-11-04), 5 commits ahead of the
  former candidate `33f48f4a`.
- **Candidate digest** (published 2026-10-31, NEVER deployed):
  `@sha256:db5a3a53a2bc8c5d9279403d95179950b13069a7c82b9568ff6a3f0e36e86dbc`
  ← `image-publish` run `41242973643` at `33f48f4a` (success, 1m27s). The
  run log shows the build step emitting `provenance,mode=max` with the run
  as builder id, then a build-provenance attestation signed and uploaded
  (read 2026-11-01). SUPERSEDED 2026-11-04: never deployed; live is now
  `e1b564af` (below).
- **Live digest** (re-read 2026-11-04, re-derived in this lane):
  `eshu-workflow-claim-plane` + `eshu-workflow-coordinator-satellite` both
  `ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4`,
  1/1 ready, still dark. The 2026-11-01 record
  (`03d9f97632e196de0f543fa7c0bcfb827db5a3ae8cb1d8e9320511f9b4bada83`
  ← run `40779192627` at `b6497fce`, no SLSA attestation) is superseded.
- Provenance of live `e1b564af` (resolved 2026-11-04): built by
  `eshu-hq/eshu-workflow` `image-publish` @ `refs/tags/v0.1.1`, run
  `41332377147` (head `8311ed69`); live attestation query on the full
  digest returns 2 Sigstore/Fulcio bundles (parsed Fulcio cert:
  workflow identity + run URL). Earlier dead ends (packages API 403,
  prefix-attestation 404) retired with the prefix-only record.
- Live in-tree coordinator image (2026-11-04 handoff):
  `eshu:sha-e3d81617@sha256:3a07c59b` (references as handed off; earlier
  `sha-bdf2823363b8…` / `sha-5eb6dd84…` figures superseded, not re-read by
  this lane).
- Dark/authoritative (2026-11-04): satellite args carry
  `--deployment-mode=dark`, no `--claims-enabled`, both deployments 1/1
  ready; the in-tree coordinator owns authority. Whether the
  deployment-repo re-pin PR `#491` merged (the 2026-11-01 flip
  precondition) NOT_CHECKED by this lane.
- Gap (2026-11-04): the tags (`09e23b93`, `8311ed69`), satellite `main`
  (`af3311e0`), and the live digest (`e1b564af` = tag `v0.1.1` build, run
  `41332377147`) are tied together above; the parity re-bind still needs
  a lane against the live record.
- Note (2026-11-04): all digest rows defer to the §2 live record above;
  §5 refreshed in this lane. Post-flip contention results ride the flip
  report, not this lane.

## 3. Oldest/newest core pairs + compat

Single-major protocol: oldest = newest = v1. Compat proof is the pin test
in §1 (oldest/newest pair collapses to one major until a new major lands
with its own compat test). `TestClaimPlaneClient*`: 8/8 PASS at this HEAD
(`go test ./internal/workflow/ -run TestClaimPlaneClient -count=1`, re-run
2026-11-01). Mutation-proven per `EXTRACTION_PREP.md` §11. Empty-batch
Enqueue/Upsert stay local no-ops matching the in-tree store; the
differential harness records both shims.

## 4. Pin/fencing

What is on main and what is not. Read this section before any claim that
"pin/fencing is done".

| Capability | Tag | Where, on `origin/main` `15caae83c5` |
| --- | --- | --- |
| Schema fence: `UNIQUE (work_item_id, fencing_token)` on `workflow_claims` | PRESENT-on-main | `go/internal/storage/postgres/migrations/014_workflow_control_plane.sql:182`, [`workflow_control_schema_sql.go:275`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/workflow_control_schema_sql.go#L275) |
| Positive-token check on a claim mutation | PRESENT-on-main | production check `mutation.FencingToken <= 0` in `validateClaimMutation` ([`workflow_control_helpers.go:344`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/workflow_control_helpers.go#L344)), called from the ingestion commit path. `Claim.Validate` ([`types.go:413`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/workflow/types.go#L413)) has the same rule but no non-test caller |
| Same-transaction `HeartbeatClaim` + persist coupling (the live fence) | PRESENT-on-main | [`ingestion.go:214-215`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/ingestion.go#L214); same shape at `generation_freshness_reproject.go:163` and `control_scanner_heartbeat.go:62` |
| Client method `ClaimPlaneClient.PinClaimForCommit` calling `/workflow.v1/ValidateAndPinClaimForCommit` | PROVEN-in-harness | [`claim_plane_client.go:395-425`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/workflow/claim_plane_client.go#L395); only tests and `workflow/parity` call it, no production caller |
| Server-side pin handler (`POST /workflow.v1/ValidateAndPinClaimForCommit`) | PROVEN-in-harness, other repo | `eshu-workflow` `server/server.go:74` at tag `v0.1.1` (`8311ed699c25201aba8d79c7a0990eaf00b42119`; re-read 2026-11-04, line 74 still registers the route) — live image `ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4` built by `image-publish` run `41332377147` at that tag (SLSA bundle invocation, re-derived 2026-11-04) with 2 Sigstore/Fulcio bundles; not in this repo. Supersedes the `33f48f4a` candidate pin (never deployed). |
| Core consumption of the pin: pin before BEGIN, re-pin for the life of the transaction, and a per (generation, work item) fence row tagged with claim, fencing token, and commit id | PRESENT at PR-B merge (#9376 P2) | `go/internal/storage/postgres/ingestion_claim_fence.go` and `claim/fence/` behind `IngestionStore.ClaimPlane` (nil keeps the in-tree path); table `generation_commit_fences` (migration 270); set from `store.Selection.Pinner` in every claim-aware collector that can select the plane; `collector.ClaimedService` refuses a mismatched control store and committer at start |
| Commit-time fence, replacing "never activate a generation below the highest activated fencing token" | PRESENT (#9376 P2, arbiter ruling 2026-11-02) | The per-scope activation rule was wrong: fencing tokens are per work item, and terraform-state can commit one resolved generation from two work items. The rule is now: a plane-claimed commit of generation G under claim (item I, token T) is durable only if the plane pinned (I, T) before BEGIN, the stored fence for (G, I) is below T or equal to T under the same claim id, and the latest pin deadline exceeds the commit time by the 5 s margin when COMMIT is sent. Fences of different work items on one generation are incomparable. Activation stays once per generation; nothing runs at projector Ack. Proof: `TestClaimPlaneFenceRefusesReclaimedStaleWriterLive`, `TestParityStaleWriterCommitFence` |

So the replacement for the same-transaction coupling is the protocol route,
the satellite handler, and (with #9376 P2) the core consumption side: the
ingestion commit pins a plane-held claim, writes the (generation, work item)
fence row, and checks the pin deadline before COMMIT, with a
failing-then-green stale-writer regression. It is inert until
`ESHU_WORKFLOW_CLAIM_PLANE_URL` is set for a collector. The earlier statement that it was "PROVEN
in-tree" described the harness only and stays withdrawn.

Static lock audit (carried from the 2026-10-30 code read; spot-checked
2026-11-01 by `FOR UPDATE` / advisory-lock location, not re-derived):
item-row-first order everywhere
([`workflow_control_sql.go:118,181,229,278`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/workflow_control_sql.go#L118),
`workflow_control_release_sql.go:21`); the one reaper inversion
(`FOR UPDATE OF claim, item SKIP LOCKED`, `workflow_control_sql.go:333`)
never waits, so no wait cycle; quota item→usage
(`workflow_control_quota_sql.go:30`); one xact-scoped advisory lock on the
open-targets create path (`workflow_control_open_targets.go:28`); dispatcher
mutex config-only. HELD: deployed-binary contention at
acceptance scope; the scoped synthetic-write grant is recorded (`#8875` comment `6558745112`, 2026-11-01); execution stays HELD on its open preconditions (the enablement IaC PR and its pre-authored revert PR are not yet linked on `#8875`, and the `#9727` authority decision is not recorded). The workload spec is `REMOVAL_CONTENTION_SPEC.md`; the
procedure link is `REMOVAL_PREP.md` §3.

## 5. Contention / fence / reclaim / replay evidence

- Claim-depth harness (`#8468`): N concurrent claimers on real Postgres.
  PROVEN-in-harness.
- Advisory-lock live test: 25/25 duplicate repro without the lock.
  PROVEN-in-harness.
- Differential parity: concurrent single-holder + fencing monotonicity on
  both legs (in-tree legs PASS on local isolated PG; satellite legs HELD —
  need `PARITY_SATELLITE_URL/DSN`; the scoped grant is recorded (`#8875` comment `6558745112`) but execution is HELD on its open preconditions; live bind target re-read 2026-11-04: `eshu-workflow-claim-plane` + `eshu-workflow-coordinator-satellite` both run `ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4` 1/1 ready, satellite args `--deployment-mode=dark` with no `--claims-enabled`, tag `v0.1.1` (`8311ed69`) via `image-publish` run `41332377147` with 2 Sigstore/Fulcio bundles; in-tree authority stays gated by the active-mode startup rule in `go/internal/coordinator/config.go`. Supersedes the 2026-11-01 record: last bind `03d9f976`, candidate `db5a3a53` never deployed, neither bound).
- Reclaim: PROVEN-in-harness. Both legs run the lease-reaper scenario
  (`leaseReaperScenario` in `go/internal/workflow/parity/reaping_test.go`,
  `TestParityConformance`), the satellite reap path is exercised there too
  (`ReapExpiredClaims` calls at lines 80 and 93), and the in-tree store has the
  integration test
  `TestWorkflowControlStoreIntegrationHeartbeatReclaimAndSplitBrain` in
  `go/internal/storage/postgres/workflow_control_claim_lifecycle_integration_test.go`.
  Deployed-binary reclaim timing NOT measured — gap.
- Replay: `go/internal/storage/postgres/claim_replay_ack.go` (`#9428`) —
  PRESENT-on-main. Identical terminal claim replays acked, classifier bound
  to the mutated claim/item pair (`e3a2203f57`); ambiguous-timeout replay
  test still cutover-gated.
- HELD (honest): deployed-binary contention at acceptance scope and
  in-cluster throughput. The scoped grant is recorded (`#8875` comment
  `6558745112`, 2026-11-01, satellite DB plus the isolated in-tree schema plus
  enumerated synthetic instances; no core QA-environment writes, no real scopes, no
  other environment). Execution stays HELD on its open preconditions: the
  enablement IaC PR and pre-authored revert PR numbers are not yet linked on
  `#8875`, and the `#9727` authority decision is not recorded. The static audit
  (§4) is unchanged and is not a substitute.

## 6. Stage-1c + README gaps

- Stage-1c LANDED: `a1c30b40be` (2026-10-28) — the six `ControlStore`
  methods the seam left as `ErrClaimPlaneUnsupported` now speak
  workflow.v1 against the 20-route table; sentinel, stubs, and the
  superseded server-internal test removed. No interim markers at this
  HEAD (zero `interim` / sentinel hits in `go/internal/workflow`,
  `go/internal/coordinator`, `go/cmd/workflow-coordinator`; not re-run for
  the 2026-11-01 refresh).
- README gaps (honest): `go/internal/workflow/README.md` documents the
  in-tree contracts but not the 20-route table, the major-1 pin, the
  `v0.1.0` tag, oldest/newest pairs, or dark/authoritative state;
  `claim-plane-client-evidence.md` covers the Stage-1b seam perf note only.
  This declaration does not backfill those READMEs — flagged for the
  cutover lane.

## 7. `#9383` disposition

Standing owner rollback policy (recorded on `#9383` / `#8876`): no
downgrade-migration rehearsals anywhere; rehearse only non-downgrade paths
(delete-path for stateless satellites); per-boundary disposition is
delete-path-suffices OR roll-forward-plus-rebuild with documented re-sync
RTO. Abort-to-dark rehearsal with ~40s re-sync RTO recorded on `#8875`
(comment `6505750834`, not re-measured here). SLSA attestation: per the
§2 live record (2026-11-04) — live digest built at tag `v0.1.1`
(run `41332377147`), 2 Sigstore/Fulcio bundles.
Live re-verification of `#9383` text was BLOCKED by gh quota on 2026-10-31
and not retried; the disposition above is cited-from-record, not re-read.

## Scorecard (honest)

| Spec item | Verdict | Proof / gap |
| --- | --- | --- |
| Protocol major 1, 15 called routes | PROVEN-in-harness | pin test, 8/8 green at `15caae83c5`; client not wired into a production binary |
| 20-route table (15 + 5) | DECLARED, pin split | 5 uncalled routes pin in `eshu-workflow` (`#17` merged), different repo/lane, not re-run here |
| Tag `v0.1.0` | EXISTS (satellite) | `09e23b93`, ls-remote 2026-11-01; not the `main` head |
| Deployed digest + dark/authoritative | NOT_CHECKED 2026-11-04 — see §2 live record | §2 re-read: live `e1b564af` dark, in-tree authoritative; `03d9f976`/`db5a3a53` figures retired; #491 merge state NOT_CHECKED |
| Oldest/newest pairs + compat | PROVEN-in-harness (v1/v1) | pin + mismatch tests |
| Schema fence + positive token | PRESENT-on-main | `014_workflow_control_plane.sql:182`, `workflow_control_helpers.go:344` |
| Same-transaction heartbeat fence | PRESENT-on-main | `ingestion.go:214-215`; still the live fence |
| Pin: client + satellite handler | PROVEN-in-harness | `claim_plane_client.go:395-425`; satellite `server/server.go:74` |
| Pin: core consumption + activation rule | **ABSENT-on-main** | no core caller, no generation tag, no highest-token rule; needs its own change |
| Deployed contention | HELD | static audit only; scoped grant recorded (`#8875` comment `6558745112`), execution HELD on IaC + revert PR numbers and the `#9727` authority decision; spec in `REMOVAL_CONTENTION_SPEC.md` |
| Reclaim/replay | PARTIAL | reclaim PROVEN-in-harness (`reaping_test.go`, claim-lifecycle integration test); replay-ack `#9428` PRESENT-on-main; deployed reclaim timing + ambiguous-timeout replay HELD |
| Stage-1c, no interim markers | LANDED / CLEAN | `a1c30b40be`; zero hits at the 2026-10-31 check |
| README gaps | FLAGGED, not filled | listed above |
| `#9383` disposition | CITED-FROM-RECORD | live re-read blocked by quota |
| SLSA attestation | PARTIAL — see §2 live record | live digest built at tag `v0.1.1` (run `41332377147`), 2 Sigstore/Fulcio bundles; 11-01 split retired |
