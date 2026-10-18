# #7638 item 1: built-binary quiet-generation drive, both arms (Refs #7638)

Status: **both arms GREEN (measured).**
No production code changed in this step. Only this file is new.
Supersedes `7638-obligation-proof-item1-blocked.md` (runtime unblocked via
the remote validation host; the resume runbook there was followed verbatim).

## Run manifest

- `run_id`: `7638-item1-drive-20261018T120000Z` (operator-local manifest kept
  on the run host per the run-manifest reference; host identity, SSH key
  paths, and other operational details stay off-repo).
- Venue: remote validation host over VPN/SSO, Docker 29.9.0.
- Base: `1664cf2f` (Merge PR #8418), compose images built from that tree,
  native Neo4j stack (`docker-compose.neo4j.yml`) + overlay: postgres host
  port `15461` (later dropped, see caveat), Neo4j bolt `7687`/HTTP `7474`,
  filesystem fixture root, synthetic `ESHU_GITHUB_ORG=item1drive` RemoteURL
  on the ingester, `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` on the
  resolution engine.
- Fixture: two filesystem repos — `deploy-source` (Flux
  `GitRepository/deploy-target` linkage + deployment manifest referencing the
  target) and `deploy-target`. Bootstrap converged 32/32, first-gen
  obligations completed.

## Flag-on arm: GREEN

Quiet mutation = host-only README append in `deploy-source`, no other commit.

| Event | Time (UTC 2026-10-18) | Evidence |
|---|---|---|
| Mutation lands | 12:29:20 | `scope_generations.observed_at` for `21eca7f8…1ce4` |
| `phase_not_ready` fence | 12:29:32 | engine `cross-repo resolution gated … reason=backward_evidence_not_committed` (same shape banked verbatim for `58bc…` at 12:35:18) |
| Backward evidence | 12:29:32.732 | `backward_evidence_committed/cross_repo_evidence` committed |
| Consumer completes | 12:29:33.435 | `deployment_mapping/service_uid` committed; obligation `completed`; engine `completed woken=1` observed pre-restart (see caveat) |

Converge took ~13 s mutation → both consumer-product phases. Normal
reduction phases (`canonical_nodes_committed`, `deployable_unit_correlation`,
`workload_materialization`) publish within seconds either way; the two rows
above are the obligation consumer's exclusive products (absent without it —
see control fingerprint).

`DEPLOYS_FROM` edge (deploy-source → deploy-target) confirmed on all three
surfaces for the flag-on generation:

- Neo4j (cypher-shell): edge present with FLUX evidence.
- API: `/api/v0/evidence/relationships/resolved_814bd4e80c105402` —
  confidence 0.99, `evidence_kinds: [FLUX_GIT_REPOSITORY_SOURCE]`.
- MCP (`POST /mcp/message`, `tools/call get_relationship_evidence`,
  `resolved_id: resolved_814bd4e80c105402`): same edge, confidence 0.99,
  `"exact/fresh — Eshu query completed."`.

## Control arm (flag off): GREEN

- Flag verified off: `docker inspect` on the resolution-engine container →
  `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=false`; off means the
  `obligation.Runner` never starts (`activation_obligation_wiring.go`), and
  the registry row documents "obligations stay pending" while off.
- First control mutation (12:35:16Z, gen `58bc87…`) was TAINTED (flag-on
  engine processed it pre-restart: gated/deferred at 12:35:18, obligation
  still `pending`) — discarded as control evidence, kept as an accidental
  second fence sample.
- Clean control mutation 12:35:50Z → gen `ea23c123…` (`observed_at`
  12:35:50.954). At +43 s: obligation `pending`; 6 phase rows, all normal
  reduction phases (`canonical_nodes_committed`,
  `deployable_unit_correlation`, `workload_materialization`) —
  **neither** `backward_evidence_committed` **nor** `deployment_mapping`
  present (the exact two rows the flag-on gen has).
- Watch: background poll every 120 s × 8 of obligation state + phase count
  for gen `ea23c123…`, final read 12:52:12Z (+16.4 min).
- Verdict: **GREEN.** All 8 polls `pending:6`; final read obligation
  `pending`, consumer-product phases `0`. The phase row for the new
  generation stayed absent and the obligation stayed pending for 16+ minutes
  — 70x past the ~13 s flag-on converge — exactly the documented flag-off
  contract ("obligations stay pending"). No `completed woken` line for this
  generation in the engine log.

## Caveats (honest, not blocking)

1. The `completed woken=1` engine log line was observed on the flag-on
   container incarnation, which was then recreated for the flag flip; its
   logs are gone. Durable DB proof stands in: obligation `completed` plus
   both consumer-product phase rows with `committed_at`. No claim rests on
   the lost line alone.
2. The postgres host-port mapping (`15461`) vanished after the engine
   recreate (cause undetermined; postgres itself not recreated).
   All post-flip PG reads go through `docker exec … psql` — same server,
   same data, no container churn.
3. `generation_id` is text (`max()` is meaningless); generation identity is
   by exact id + `observed_at` ordering throughout this doc.
