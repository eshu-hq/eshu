# Assumes IAM Role Family Evidence (#6228)

No-Regression Evidence: the family landing adds one ODU (41 facts), one
committed cassette, one 2-edge expected fixture plus one 10-edge
shared-cell live variant, one guard file plus one duplicate-convergence
test, one registry row, pins, and fault trio cells; the only production
`.go` touches are three identifier renames in the secrets graph writer
(`secretsIAM*` to `SecretsIAM*`, zero template-text bytes changed -- every
`git diff` hunk pair is identical after normalizing the identifier case)
and one entry registering the assumes edge type in the existing
single-type registry holder (static startup data, same shape the sibling
commit established). No Cypher template, writer logic, extractor logic,
worker, queue, lease, batch size, or transaction changed, so live graph
truth and write volume are unchanged -- the new rows flow through the
existing UNWIND/MERGE path the authenticates_vault_role family already
proves.

- Baseline: origin/main 3e07d26b1f (family waived: no ODU, guard,
  fixture, cassette, registry row, or registered edge types).
- After: `go test ./internal/ifa/... ./internal/storage/cypher/...`
  green (exit 0); determinism static mirror pass (exit 0);
  fault-injection static mirror: the assumes stub cases pass (execution
  progresses past them under set -e), the full mirror stays red on a
  repo_dependency telemetry stub-timing case that fails identically on the
  bare base in this environment (pre-existing, load-driven, unrelated to
  this family); drift lockstep green.
- Backend/version: hermetic guard (no backend) plus gate-owned disposable
  Compose stacks on the PR wave (Postgres + Neo4j/NornicDB, live matrices
  not yet run from this lane).
- Input shape: assumes ODU (6 lanes: IRSA + Pod Identity exact chains,
  fingerprint-only, partial-chain, Deny, wildcard-trust non-producers;
  lane A's second bound Vault role proving duplicate delivery converges;
  4 endpoint observations); 2-edge set, 10-edge shared-cell set.
- Terminal counts: guard green on the 2-edge set; duplicate-convergence
  test green (2 chains, 1 edge; mutation-verified RED on a single role);
  cassette lockstep green; shared-cell union green (authenticates live
  file 8 to 11 edges).

No-Observability-Change: no spans, metrics, logs, ports, or status fields
added. Family failures report through the existing guard and gate signals.

## Live evidence (2026-11-03, rebased head e25dd55c83 on origin/main 50a0ddd333)

- Determinism: `scripts/verify-ifa-determinism.sh` exit 0. Cross-N
  canonical digest
  `213ddc5f089b5f47b83ae6e0bb32bea8db913321a3a60042a45601d1202ffbc4`
  byte-identical for N=1 (wall 258s), N=2 (188s), N=4 (250s). The
  shared N-cell asserts the ten-edge live combined set for
  assumes_iam_role exactly, pre- and post-delta, beside every sibling
  shared-cell family.
- Fault trio: `scripts/verify-ifa-fault-injection.sh --shard 3/8` exit 0
  (shard PASS, project eshu-ifa-fault-injection-3864862). Trio digest
  `09db83fa64d348150cc859e4e570c63465115c4c2c2517565e000eaee7656461`
  byte-identical across baseline, kill, and failgraphwrite:
  - baseline_assumes_iam_role: wall 77s, 2-edge exact set, 0
    dead_letter rows, fault-free secrets_iam_graph_projection
    retry-attempt baseline 0.
  - killassumesiamrole (per-statement hold, kill -9 mid-hold): wall 87s,
    2-edge exact set, 0 dead_letter rows. Hold non-vacuous:
    the statement-held marker was present before the kill, and the
    replacement reducer re-attempted the projection domain
    (attempts 1 > baseline 0).
  - failgraphwriteassumesiamrole: wall 78s, 2-edge exact set, 0
    dead_letter rows.
- Backend: gate-owned disposable Compose stacks per cell (Postgres +
  NornicDB), `ESHU_DEPLOY_CHECKOUT=/tmp/eshu-deploy`, held the serial
  live-drive token for the whole run.
