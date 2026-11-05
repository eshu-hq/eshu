# Cutover acceptance case — eshu#8877 (DRAFT, Leaf 8)

Status: DRAFT. Verdict belongs to the gate owner. Two boxes are
placeholders pending Leaf 6 (oldest/newest record) and Leaf 7
(rollback rehearsal). Do not cite this file as acceptance until
those land and the draft flag is removed.

Scope proven so far: zero-fact synthetic vehicle on the designated QA cluster only.
Every live claim below is bounded by the window grant that expired
2026-11-05T19:18Z (recorded on #8877). Positive-fact scopes are
explicitly NOT PROVEN and need a future window.

## Criterion map

| # | Issue criterion | State | Evidence |
| --- | --- | --- | --- |
| 1 | Inventory manifest/trust/install/execution, result limits, policy controls, host-to-core/workflow protocols | Met | PREP §§1–6; host PROVENANCE.md + README.md |
| 2 | Revoked/expired artifacts, sandbox/resource bounds, cancellation, malformed results, credential redaction, grant-before-admission | Met in-tree + hostile parity on cutover path | PREP §7 sites; #10152 (expired-grant emission deny); #10187 hostile rows row-for-row |
| 3 | Named implementation, public-contract, release, deployment owners; actual production target/config source | Met | #8877 owner confirmation; the extension-host repository; the designated QA cluster + deployment overlays |
| 4 | Current packages, fact kinds, consumers, durable-state ownership, protocols, exact artifacts, migration stage, prerequisites | Met | PREP §§1–6, 8–10 |
| 5 | Behavior + public-contract parity, representative + hostile, source identity/truth/redaction preserved | Met test-only | #10187 (9/9 emission + 9/9 source parity); #10152 |
| 6 | Extracted deployed path preserves reducer/graph/API/MCP truth; unaffected surfaces identified | Partial (zero-fact scope) | Window flip: 9×60s idle claim cycles, zero facts/errors; freeze-1 18:25:54Z + freeze-2 18:26:33Z; census clean. Positive-fact truth NOT PROVEN — future window. |
| 7 | Concurrent retry, duplicate delivery, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures, no lost facts | Partial | #10196 test-only mix (8 workers × 60, race clean). Live digest-bound proof PENDING — fresh window grant needed. |
| 8 | Bounded resource use, no regression, health/readiness/freshness/metrics/traces/logs/status | Partial | #10090 observer backing; dark /healthz mode=dark v0.1.0 live; S1 at freeze base. Live bounds numbers PENDING — future window. |
| 9 | Credential handling, artifact provenance, vuln response, oldest/newest core↔component pairs | PLACEHOLDER — Leaf 6 | Leaf 6 lane (doc-only degenerate 1×1 matrix per arbiter verdict) in flight; vuln side: per-arch Trivy scan in host release pipeline (Leaf 2 proof). |
| 10 | Independent build/release/deploy on public contracts, no core handles/shared-table shortcuts | Met | Host v0.1.0 released (SLSA+SBOM) + dark live on the designated QA cluster at the pinned digest (Leaf 2). |
| 11 | Rollback rehearsal on exact deployed digest (PREP §8 verbatim, measured RTO); fence/revoke/replay, no dual authority, no lost facts | PLACEHOLDER — Leaf 7 | Nearest executed neighbor: return-to-dark #550 (merged bf92b10 + synced, window objects absent, fleet steady) — a return, not the §8 rehearsal. |
| 12 | Independent review, required local proof, stable exact-head CI; in-tree removal only after production acceptance | Process held per leaf; removal NOT STARTED | Per-leaf reviews + attest receipts on #8877; Leaf 9 terminal behind this verdict. |

## Explicitly not claimed

- Positive-fact emission through the extracted path (criteria 6–8 live halves).
- Enabled-flow host×core compatibility beyond the single-major epoch (Leaf 6 expiry triggers).
- In-tree removal (Leaf 9; needs this acceptance + standing-authoritative declaration).

## Reopen triggers

Any of: Leaf 6 expiry trigger fires; a window run shows a non-zero fact or error on the vehicle scope; fleet digest moves without re-baseline; second host release tag cut.
