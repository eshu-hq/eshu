# Cutover acceptance case — eshu#8877 (DRAFT, Leaf 8)

Status: assembled for the gate owner's verdict. Leaf 6 (pairs record)
and Leaf 7 (rollback rehearsal) evidence below; the verdict still
belongs to the gate owner.

Scope proven so far: zero-fact synthetic vehicle on the designated QA cluster only.
Every live claim below is bounded by the window grant that expired
2026-11-05T19:18Z (recorded on #8877). Positive-fact scopes are
explicitly NOT PROVEN and need a future window.

## Criterion map

| # | Issue criterion | State | Evidence |
| --- | --- | --- | --- |
| 1 | Inventory manifest/trust/install/execution, result limits, policy controls, host-to-core/workflow protocols | Met | PREP §§1–6; host PROVENANCE.md + README.md |
| 2 | Revoked/expired artifacts, sandbox/resource bounds, cancellation, malformed results, credential redaction, grant-before-admission | Met in-tree + hostile parity on cutover path | PREP §7 sites; #10152 5787b88f (expired-grant emission deny); #10187 d762ecf4 hostile rows row-for-row |
| 3 | Named implementation, public-contract, release, deployment owners; actual production target/config source | Met | #8877 owner confirmation; the extension-host repository; the designated QA cluster + deployment overlays |
| 4 | Current packages, fact kinds, consumers, durable-state ownership, protocols, exact artifacts, migration stage, prerequisites | Met | PREP §§1–6, 8–10 |
| 5 | Behavior + public-contract parity, representative + hostile, source identity/truth/redaction preserved | Met test-only | #10187 d762ecf4 (9/9 emission + 9/9 source parity); #10152 5787b88f |
| 6 | Extracted deployed path preserves reducer/graph/API/MCP truth; unaffected surfaces identified | Partial (zero-fact scope) | Fresh window: 9 consecutive 60s idle claim cycles 23:31:42–23:39:42Z on the pinned vehicle digest, zero facts/errors, coordinator planned the vehicle instance; freezes 23:33:40Z + 23:33:55Z, 40/40 deployments ready, Synced/Healthy. Positive-fact truth NOT PROVEN — future window. |
| 7 | Concurrent retry, duplicate delivery, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures, no lost facts | Partial | #10196 87fa460c test-only mix (8 workers × 60, race clean). Live: 9 idle claim cycles with duplicate-plan skip (target_already_planned) and zero loss; revocation/staleness live halves need a positive-fact window. |
| 8 | Bounded resource use, no regression, health/readiness/freshness/metrics/traces/logs/status | Partial | #10090 20c8ba24 observer backing; dark /healthz mode=dark v0.1.0 live. Fresh window: worker 1/1 within the vehicle resource envelope, fleet steady, no restarts. Live bounds numbers beyond the zero-fact envelope PENDING — future window. |
| 9 | Credential handling, artifact provenance, vuln response, oldest/newest core↔component pairs | Met for the single-major epoch | Leaf 6: degenerate 1×1 matrix (host v0.1.0 × core-major 1, both directions same cell) with re-derivation commands + verbatim expiry triggers, byte-compared across both repos (eshu #10202 60f2fe256d + host #4 7ebc7db, both merged); vuln side: per-arch Trivy scan in host release pipeline (Leaf 2 proof). Matrix auto-expires on second host tag, next major, host dependency, or pin bump. |
| 10 | Independent build/release/deploy on public contracts, no core handles/shared-table shortcuts | Met | Host v0.1.0 released (SLSA+SBOM) + dark live on the designated QA cluster at the pinned digest (Leaf 2). |
| 11 | Rollback rehearsal on exact deployed digest (PREP §8 verbatim, measured RTO); fence/revoke/replay, no dual authority, no lost facts | Met (zero-fact scope) | Fresh window: pre-state recorded (fleet digest, sync revision, 40/40 ready); fence via revert merge, revoke via grant expiry, path deleted to first claim, restore = dark main; measured RTO 2m19s (revert-merge to verified dark 23:42:19Z, all window objects absent, Synced/Healthy, fleet steady, zero post-return vehicle lines). No dual authority (single enable at a time); no lost facts (zero-fact scope, census clean). |
| 12 | Independent review, required local proof, stable exact-head CI; in-tree removal only after production acceptance | Process held per leaf; removal NOT STARTED | Per-leaf reviews + attest receipts on #8877; Leaf 9 terminal behind this verdict. |

## Explicitly not claimed

- Positive-fact emission through the extracted path (criteria 6–8 live halves).
- Enabled-flow host×core compatibility beyond the single-major epoch (Leaf 6 expiry triggers).
- In-tree removal (Leaf 9; needs this acceptance + standing-authoritative declaration).

## Reopen triggers

Any of: Leaf 6 expiry trigger fires; a window run shows a non-zero fact or error on the vehicle scope; fleet digest moves without re-baseline; second host release tag cut.
