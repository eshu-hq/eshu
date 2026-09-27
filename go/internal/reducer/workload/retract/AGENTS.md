# AGENTS.md — internal/reducer/workload/retract

Scoped instructions for this package. The root `AGENTS.md` still applies.

- This is a reducer family package. It may import `internal/facts`,
  `internal/telemetry`, `reducer/payloadcore`, and the standard library. It
  must never import the parent `internal/reducer` package; the graph port is
  the local `Executor` interface.
- Keep both statements anchored on `Repository {id}` and scoped by
  `evidence_source`. Removing the provenance predicate would delete other
  writers' `DEFINES`; unanchoring would scan the relationship type store-wide.
- Never bind a nil or missing keep-list key: `NOT (x IN null)` is null and the
  retract silently deletes nothing.
- Never retract a repository outside `FullGenerationRepositoryIDs`. A delta
  generation's candidates are partial, so an absent workload there is not
  evidence of removal.
- The zero-row `DELETE` cost on NornicDB (NornicDB#296, `nornicdb-pitfalls.md`)
  is unmeasured for this id-anchored shape. Measure it before adding a probe
  guard.
