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
