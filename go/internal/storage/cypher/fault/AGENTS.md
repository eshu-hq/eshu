# AGENTS.md — storage/cypher/fault guidance for LLM assistants

Namespace-only parent. It owns no implementation: `doc.go` holds the
package clause and nothing else.

## Read first

1. `go/internal/storage/cypher/fault/executor/README.md` — the decorator,
   its build-tag split, and its wiring
2. `go/internal/replay/faultreplay/` — the fault-script vocabulary this
   subtree applies, owned outside cypher
3. `go/cmd/reducer/ifa_fault_wiring.go` — the only production wiring that
   installs the decorator (build tag `ifafaultinjection`)

## Rules

- Do not add implementation to this directory. New fault seams belong in a
  new leaf package beside `executor/`, importing the parent `cypher`
  contract, never the reverse: `cypher` must not import this subtree or the
  Executor seam gains a cycle.
- Keep every leaf's `doc.go`, `README.md`, and `AGENTS.md` accurate when
  moving files here.
