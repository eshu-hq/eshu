# workflow.v1 plane declaration for `#9376` (`#8877` declaration lane)

Status: **DRAFT DECLARATION — NOT A CUTOVER CLAIM**. No authority moves on
this file. The in-tree coordinator owns claims, leases, and scheduler state
until a standing cutover claim; the satellite owns nothing. Removal stays HELD
per `REMOVAL_PREP.md` §7.

Assembled at `origin/main` `15caae83c5` (2026-11-01) from read-only
evidence. Every scored item below names its proof or its gap. Gaps stay
gaps: acceptance is contingent on the spec match, and this draft does not
tick boxes it cannot prove.

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

## 2. Tag + deployed digest + dark/authoritative

- Tag: `eshu-workflow` `v0.1.0` EXISTS at
  `09e23b93ad53c3c0acffd1c518f0439e7e0407fa` (re-verified 2026-11-01, read-only
  `git ls-remote --tags`). It is no longer the `main` head. No `v0.1.0` tag
  exists on `eshu-hq/eshu` (only `v0.0.x` + `sdk/go/*` tags) — the
  declaration tag is satellite-scoped by design.
- Satellite `main` is `33f48f4a6b0fa6be8038b5282da502585ef72874`
  (2026-11-01, `ci(image-publish): publish on version tags and every main
  merge (#18)`), 22 commits ahead of `b6497fce`, the commit behind the live
  digest.
- **Candidate digest** (published, NOT deployed):
  `@sha256:db5a3a53a2bc8c5d9279403d95179950b13069a7c82b9568ff6a3f0e36e86dbc`
  ← `image-publish` run `41242973643` at `33f48f4a` (success, 1m27s). The
  run log shows the build step emitting `provenance,mode=max` with the run
  as builder id, then a build-provenance attestation signed and uploaded.
  The earlier "SLSA attestation absent" gap is closed for this digest only.
- **Live (dark) digest**: `eshu-workflow-claim-plane` +
  `eshu-workflow-coordinator-satellite` both
  `@sha256:03d9f97632e196de0f543fa7c0bcfb827db5a3ae8cb1d8e9320511f9b4bada83`
  ← publish run `40779192627` at `b6497fce` (merge of `eshu-workflow#7`).
  That image has no SLSA attestation. Per the coordinator handoff of
  2026-11-01 the live dark pods are still on `03d9f976`; this lane did not
  re-read the cluster. They move to `33f48f4a` only when the private
  deployment-repo re-pin PR `#491` ("re-pin dark workflow claim-plane and
  satellite to 33f48f4a") merges; it is OPEN as of 2026-11-01 (`gh pr view`).
- Live in-tree coordinator image: `eshu:sha-bdf2823363b8…` per the same
  handoff (earlier drafts recorded `sha-5eb6dd84…`; superseded, not
  re-read by this lane).
- Dark/authoritative: satellite args carry `--deployment-mode=dark`, no
  `--claims-enabled`, 0 restarts; the in-tree coordinator owns authority.
  Last read-only cluster check 2026-10-31. Post-revert dark state holds.
- Gap: the tagged commit `09e23b93`, the candidate `33f48f4a`, and the live
  `b6497fce` are three different commits. The next flip needs the `#491`
  merge, then a parity re-bind to `db5a3a53` (procedure in `REMOVAL_PREP.md`
  §4).

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
| Server-side pin handler (`POST /workflow.v1/ValidateAndPinClaimForCommit`) | PROVEN-in-harness, other repo | `eshu-workflow` `server/server.go:74` at `33f48f4a`; not in this repo |
| Core consumption of the pin: tagging the generation with claim, fencing token, and commit id | ABSENT-on-main | no non-test hit for a pin caller or a generation commit-id tag in `go/internal/storage` or `go/internal/projector` |
| Core rule "never activate a generation below the highest activated fencing token" | ABSENT-on-main (generation-activation path only) | no fencing-token comparison in the generation activation path: `go/internal/storage/postgres/ingestion.go` and `ingestion_queries.go` (generation upsert `activated_at`, line 154) hold none; the only fencing hit there is the read-side column select at [`ingestion_queries.go:31`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/ingestion_queries.go#L31). Fencing-token ordering does exist elsewhere in core and is not this rule: the `supply_chain_impact` write-admission watermark (`migrations/154_supply_chain_impact_write_admission.sql`), the AWS scan-status `(generation_id, fencing_token)` pin ([`scan_status.go:26`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/cloud/aws/scan_status.go#L26)), crossplane `claim_fencing_token` state, and the fact-upsert highest-token dedupe ([`facts.go:330-333`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/facts.go#L330)) |

So the replacement for the same-transaction coupling exists as a protocol
route and a satellite handler, and the in-tree client can call it, but core
does not call it and does not enforce the activation rule. Until a core change
lands the consumption side and its failing-then-green regression, the
pin/fencing replacement is **not on main**. The earlier statement that it was
"PROVEN in-tree" described the harness only and is withdrawn.

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
  need `PARITY_SATELLITE_URL/DSN`; the scoped grant is recorded (`#8875` comment `6558745112`) but execution is HELD on its open preconditions; last bind
  `03d9f976`; the candidate `db5a3a53` has not been bound).
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
(comment `6505750834`, not re-measured here). SLSA attestation: present on
the candidate digest `db5a3a53` (§2), absent on the live digest `03d9f976`.
Live re-verification of `#9383` text was BLOCKED by gh quota on 2026-10-31
and not retried; the disposition above is cited-from-record, not re-read.

## Scorecard (honest)

| Spec item | Verdict | Proof / gap |
| --- | --- | --- |
| Protocol major 1, 15 called routes | PROVEN-in-harness | pin test, 8/8 green at `15caae83c5`; client not wired into a production binary |
| 20-route table (15 + 5) | DECLARED, pin split | 5 uncalled routes pin in `eshu-workflow` (`#17` merged), different repo/lane, not re-run here |
| Tag `v0.1.0` | EXISTS (satellite) | `09e23b93`, ls-remote 2026-11-01; not the `main` head |
| Deployed digest + dark/authoritative | VERIFIED 2026-10-31, with drift | live `03d9f976` dark, in-tree authoritative; candidate `db5a3a53` (= `33f48f4a`, SLSA) published, not deployed, waits on the deployment-repo re-pin PR `#491` (OPEN) |
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
| SLSA attestation | PARTIAL | present on candidate `db5a3a53`; absent on live `03d9f976` |
