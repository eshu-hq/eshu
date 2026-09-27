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
- Keep the guard: with a `Reader`, delete only the stale ids the read found,
  and send no `DELETE` when nothing is stale (NornicDB#296: a zero-row
  relationship `DELETE` costs proportional to store size). The read must keep
  the keep-list delete's `MATCH`/`WHERE`, and its `UNWIND` variable must not
  share a name with a `RETURN` alias (#6786 shape X9).
- A missing reader or a failed read runs the keep-list deletes (fail toward
  deleting). Never turn a read failure into a skipped retract.
