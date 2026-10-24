# Grants Secret Read Family Evidence (#8539)

No-Regression Evidence: the family landing adds one ODU (64 facts), one
committed cassette, one 3-edge expected fixture, one guard file, one
registry row, and fault trio cells; the only production `.go` touch is
three compile-time aliases registering the grants edge/node types in the
existing single-type registry holder (same shape the sibling commit
established). No Cypher template, writer logic, extractor logic, worker,
queue, lease, batch size, or transaction changed, so live graph truth and
write volume are unchanged — the new rows flow through the existing
UNWIND/MERGE path the uses_vault_policy family already proves.

- Baseline: origin/main a630c3598b (family waived: no ODU, fixture, or
  registered edge types).
- After: `go test ./internal/ifa/... ./internal/storage/cypher/... ./internal/reducer/...` green; fault-injection mirror 103 cells green; determinism fixtures resolve; drift lockstep green.
- Backend/version: hermetic guard plus gate-owned disposable Compose stack (Postgres + NornicDB) on the PR wave.
- Input shape: grants ODU (8 lanes: IRSA + Pod Identity exact chains, 4 chain negatives, list-only, missing-KV); 3-edge set.
- Terminal counts: guard green on the 3-edge set; fault mirror 103-cell exact cover.

No-Observability-Change: no spans, metrics, logs, ports, or status fields
added. Family failures report through the existing guard and gate signals.
