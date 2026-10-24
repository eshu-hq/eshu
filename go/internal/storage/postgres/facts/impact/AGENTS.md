# AGENTS.md — storage/postgres/facts/impact guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `docs/internal/evidence/8880-supply-chain-impact-id-resolution.md` for the
   census, the per-index justification, and the measured plans.
3. `../../facts_active_supply_chain_impact.go` for the loader that runs the
   catalog.

## Invariants

- Keep the package a leaf: it renders SQL and runs none. Never import the
  parent `postgres` package.
- Every statement reads `fact_records` only inside the MATERIALIZED
  `candidate` CTE, through its anchor. Do not inline the active-generation
  join into it: the planner misestimates that two-column join at one row and
  walks every active scope (#8880 measured 55,109 buffers for 80 rows).
- A key probe's literal kind list must equal or be a subset of its anchor's
  predicate list (`TestBranchKindsAreProvableByAnchors`).
- The guard statement and the JS/TS file branches repeat their index
  predicates byte for byte (`TestGuardPredicateMatchesIndex`). Edit
  `GuardPredicate` or `JSTSLanguagePredicate` only together with a new
  migration.
- Never edit a shipped migration. A new anchor is a new numbered migration
  holding one `CREATE INDEX CONCURRENTLY`, generated from `NewIndexes`
  (`TestNewIndexStatementsMatchMigrations`), plus the checksum manifest,
  ordered-name list, and golden digest pins in the parent and `migrations/`.
- Exactly one branch returns `vulnerability.suppression` and it is the only
  statement with a LIMIT (`TestExactlyOneBranchReturnsSuppressions`). Core
  evidence is never bounded.
- Deployment context (`environment`, `workload_id`, `service_id`) never
  becomes a discovery predicate, the guard included.
- A kind changes class only with a new census recorded in the evidence note.

## Verification

```bash
cd go && go test ./internal/storage/postgres/facts/impact -count=1
cd go && go test ./internal/storage/postgres -run 'SupplyChainImpact' -count=1
# live differential, budget, and plan proofs (disposable database):
ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN=postgres://.../postgres \
ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE=1 \
  go test ./internal/storage/postgres -run 'TestSupplyChainImpactActiveEvidence' -count=1
```
