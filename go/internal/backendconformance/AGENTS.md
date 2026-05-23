# internal/backendconformance Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `matrix.go` and `specs/backend-conformance.v1.yaml` before changing the
   capability matrix.
3. `corpus.go` before changing read or write cases.
4. `profile_matrix.go` before changing NornicDB promotion gates.
5. `live_test.go` and `scripts/verify_backend_conformance_live.sh` before
   changing live backend coverage.

## Local Rules

- Default tests must stay deterministic and live-database-free. Live Neo4j and
  NornicDB proof stays behind the `live` build tag and opt-in script.
- Treat the YAML matrix as source of truth. Production code must parse it
  through `ParseMatrix`; do not hand-build production matrix values.
- Keep `ReadCase` and `WriteCase` as the behavior units. Runners must return a
  report with per-case results instead of hiding later failures behind the
  first failed case.
- Use `PhaseGroupExecutor` for cases that need phased write visibility. Do not
  flatten those cases into ordinary execute calls.
- Keep canonical file/entity containment in the write and read corpora because
  it proves the source-local projection shape both supported graph backends
  must share.

## Change Gates

- Matrix changes require enum/parser tests and a matching update to
  `docs/public/reference/backend-conformance.md` when the public contract
  changes.
- New read cases require deterministic local tests. New write cases require
  local tests plus live opt-in proof against the intended backend lanes.
- Profile-gate changes require the consuming runtime or Compose tests that
  assert the gate.

## Do Not Change Without Owner Review

- The v1 matrix spec format.
- Default corpus case names or semantics used by other packages.
- Live-test opt-in boundaries.
