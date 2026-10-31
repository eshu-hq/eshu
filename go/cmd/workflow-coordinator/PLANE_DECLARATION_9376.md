# workflow.v1 plane declaration for `#9376` (`#8877` declaration lane)

Status: **DRAFT DECLARATION — NOT A CUTOVER CLAIM**. No authority moves on
this file. The in-tree coordinator owns claims, leases, and scheduler state
until a standing cutover claim; the satellite owns nothing. Removal stays
HELD per `REMOVAL_PREP.md` §7.

Assembled at `origin/main` `ebffd12e26` (2026-10-31) from read-only
evidence. Every scored item below names its proof or its gap. Gaps stay
gaps: acceptance is contingent on the spec match, and this draft does not
tick boxes it cannot prove.

Claim note (gh quota 403 on 2026-10-31, retried once): lane claim text —
"`#8877` declaration lane claims `go/cmd/workflow-coordinator/PLANE_DECLARATION_9376.md`
only; sibling lanes untouched" — posts to the epic when quota allows.

## 1. Protocol major 1 + 20-route table

Protocol `workflow.v1`, major pinned to `"1"` on every client route through
the shared `(*ClaimPlaneClient).post` path (sole `Workflow-Protocol-Version`
setter). Proof: `go/internal/workflow/protocol_version_test.go`,
`TestClaimPlaneClientPinsProtocolMajorOne` — all 15 exercised routes
observed with major `"1"`; reflect pin fails the build if any exported
`ClaimPlaneClient` method leaves the list. `TestClaimPlaneClientVersionMismatchIsInvalidNotRejected`
proves a 409 `version_mismatch` maps to invalid, never to a fence rejection
(`ErrClaimRejected`).

15 called routes (in-tree client, all pinned): `ClaimNextEligible`,
`HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`,
`FailClaimTerminal`, `PinClaimForCommit` (`ValidateAndPinClaimForCommit`),
`CheckClaimPacing`, `ListCollectorInstances`, `CreateRun`,
`EnqueueWorkItems`, `ReconcileCollectorInstances`,
`UpsertCompletenessStates`, `ReconcileWorkflowRuns`, `ReapExpiredClaims`.

5 uncalled routes (no in-tree client; pin must land in `eshu-workflow`):
read plane `GetRunStatus`, `ListRuns`, `GetRunCompleteness`,
`QueueDepthSnapshot`; report plane `ReportRunProgress`.

## 2. Tag + deployed digest + dark/authoritative

- Tag: `eshu-workflow` `v0.1.0` EXISTS at
  `09e23b93ad53c3c0acffd1c518f0439e7e0407fa` (= satellite `main`,
  2026-10-29; verified 2026-10-31 via read-only `git ls-remote --tags`).
  No `v0.1.0` tag exists on `eshu-hq/eshu` (only `v0.0.x` + `sdk/go/*`
  tags) — the declaration tag is satellite-scoped by design.
- Deployed digest: `eshu-workflow-claim-plane` +
  `eshu-workflow-coordinator-satellite` both
  `@sha256:03d9f97632e196de0f543fa7c0bcfb827db5a3ae8cb1d8e9320511f9b4bada83`
  ← publish run `40779192627` at `b6497fce` (merge of `eshu-workflow#7`).
- GAP (honest): deployed digest `03d9f976` is NOT the tagged commit.
  Satellite `main`/tag is 18 commits ahead of `b6497fce`. Next flip needs
  a fresh digest + parity re-bind (procedure in `REMOVAL_PREP.md` §4).
- Dark/authoritative: satellite args carry `--deployment-mode=dark`, no
  `--claims-enabled`, 0 restarts; in-tree `eshu:sha-5eb6dd84…` owns
  authority. Re-verified read-only 2026-10-31. Post-revert dark state holds.

## 3. Oldest/newest core pairs + compat

Single-major protocol: oldest = newest = v1. Compat proof is the pin test
in §1 (oldest/newest pair collapses to one major until a new major lands
with its own compat test). `TestClaimPlaneClient*`: 8/8 PASS at this HEAD
(`go test ./internal/workflow/ -run TestClaimPlaneClient -count=1`).
Mutation-proven per `EXTRACTION_PREP.md` §11. Empty-batch
Enqueue/Upsert stay local no-ops matching the in-tree store; the
differential harness records both shims.

## 4. Pin/fencing

- `UNIQUE(work_item_id, fencing_token)` on `workflow_claims`; fencing
  token must be positive (`ClaimStatus.Validate`).
- `ValidateAndPinClaimForCommit` pins the claim server-side so the reaper
  cannot expire it before the commit deadline; core tags the generation
  with claim/fencing/commit id and never activates a generation below the
  highest activated fencing token (replaces the same-transaction
  `HeartbeatClaim`+persist coupling at `storage/postgres/ingestion.go`
  that a cross-service protocol cannot share).
- Static lock audit at this HEAD: item-row-first order everywhere;
  the one reaper inversion (`FOR UPDATE OF claim, item SKIP LOCKED`)
  never waits, so no wait cycle; quota item→usage; one xact-scoped
  advisory lock on the open-targets create path; dispatcher mutex
  config-only. HELD for a write grant: deployed-binary contention at
  acceptance scope (see §5).

## 5. Contention / fence / reclaim / replay evidence

- Claim-depth harness (`#8468`): N concurrent claimers on real Postgres.
- Advisory-lock live test: 25/25 duplicate repro without the lock.
- Differential parity: concurrent single-holder + fencing monotonicity on
  both legs (in-tree legs PASS on local isolated PG; satellite legs HELD —
  need `PARITY_SATELLITE_URL/DSN` + synthetic-write grant, last bind
  `03d9f976`).
- Reclaim: reaper path covered by `ReapExpiredClaims` pin + reap-instrument
  split (`#9659`); deployed-binary reclaim timing NOT measured — gap.
- Replay: `storage/postgres/claim_replay_ack.go` (`#9428`) — identical
  terminal claim replays acked, classifier bound to the mutated claim/item
  pair (`e3a2203f57`); ambiguous-timeout replay test still cutover-gated.
- HELD (honest): deployed-binary contention at acceptance scope and
  in-cluster throughput need their own synthetic-write grant per the
  2026-10-30 proof-closure ruling; static audit (§4) is unchanged and is
  not a substitute.

## 6. Stage-1c + README gaps

- Stage-1c LANDED: `a1c30b40be` (2026-10-28) — the six `ControlStore`
  methods the seam left as `ErrClaimPlaneUnsupported` now speak
  workflow.v1 against the 20-route table; sentinel, stubs, and the
  superseded server-internal test removed. No interim markers at this
  HEAD (zero `interim` / sentinel hits in `go/internal/workflow`,
  `go/internal/coordinator`, `go/cmd/workflow-coordinator`).
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
(comment `6505750834`, not re-measured here). SLSA attestation absent —
gap. Live re-verification of `#9383` text BLOCKED by gh quota on
2026-10-31; disposition above is cited-from-record, not re-read.

## Scorecard (honest)

| Spec item | Verdict | Proof / gap |
| --- | --- | --- |
| Protocol major 1, 15 called routes | PROVEN | pin test, 8/8 green at `ebffd12e26` |
| 20-route table (15 + 5) | DECLARED, pin split | 5 uncalled routes pin in `eshu-workflow`, different repo/lane |
| Tag `v0.1.0` | EXISTS (satellite) | `09e23b93`, ls-remote 2026-10-31 |
| Deployed digest + dark/authoritative | VERIFIED, with drift | `03d9f976`, dark, in-tree authoritative; 18-commit drift = gap |
| Oldest/newest pairs + compat | PROVEN (v1/v1) | pin + mismatch tests |
| Pin/fencing | PROVEN in-tree; deployed contention HELD | static audit + harnesses; write grant missing |
| Reclaim/replay | PARTIAL | replay-ack `#9428` landed; deployed reclaim timing + ambiguous-timeout replay HELD |
| Stage-1c, no interim markers | LANDED / CLEAN | `a1c30b40be`; zero hits |
| README gaps | FLAGGED, not filled | listed above |
| `#9383` disposition | CITED-FROM-RECORD | live re-read blocked by quota |
| SLSA attestation | ABSENT | gap, unchanged |
