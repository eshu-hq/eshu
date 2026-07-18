# Evidence: shell-exec, documentation, and taint retracts run auto-commit; orphan cleanup uses a COUNT subquery (#4367, #5116 siblings)

## Problems (all measured on the pinned v1.1.11 before the fix)

1. **Shell-exec retract under-applied.** `executeShellExecRetractStatements`
   grouped the edge DELETE and the orphan ShellCommand cleanup through
   `ExecuteGroup`. Live regression: `retract: in-scope EXECUTES_SHELL gone:
   count = 1, want 0` — the first grouped statement never applied, the same
   failure #5128/#5146 measured on the SQL and repo-dependency retracts. The
   documentation delta retract executor shared the identical grouped dispatch
   for its section-uid and document-id statements.
2. **TAINT_FLOWS_TO retract was a no-op even as a single statement.**
   `RetractCodeInterprocEvidence`, `RetractCodeInterprocEvidenceSource`, and
   `RetractStaleCodeInterprocEvidence` dispatched through the grouped
   `dispatch` path. Probed: the exact retract statement auto-committed over
   Bolt HTTP deletes the edge, while the live test through the managed
   transaction left it in place (`retract: in-scope TAINT_FLOWS_TO gone:
   count = 1, want 0`). A single DELETE in a managed transaction under-applies
   on v1.1.11; the ByUIDs variants already used the sequential
   `dispatchRetract`.
3. **The orphan ShellCommand cleanup deleted nothing.** Probed: the
   `WHERE ... AND NOT (target)--()` negated pattern predicate matches zero
   rows on v1.1.11, and an `OPTIONAL MATCH (target)-[link]-() WITH target,
   link WHERE link IS NULL DELETE target` pipeline returns the filtered row
   but does not apply the trailing DELETE when the node previously had
   (since-deleted) relationships. A `COUNT { (target)--() } = 0` predicate did
   delete the seeded orphan on both v1.1.11 and the `neo4j:2026-community`
   lane at the time this was written, but that test only proved the DELETE
   fired on a node already known to be an orphan -- it never proved the
   predicate excludes a connected node. **This claim was wrong.** The #5147
   investigation later proved `COUNT { (n)--() } = 0` is a tautology on both
   pinned NornicDB backends: the subquery's count is always reported as 0, so
   the predicate matches every node regardless of relationship state,
   including connected ones. See
   `docs/public/reference/nornicdb-pitfalls.md` ("Every Relationship-Existence
   Predicate Is Mis-Evaluated") for the corrected finding and
   `evidence-5147-orphan-sweep-antijoin.md` for the anti-join replacement that
   never relies on a relationship-existence predicate. The `ShellCommand`
   orphan cleanup in `edge_writer_shell_exec.go` still carries this predicate
   and is not fixed by #5147 (out of scope: that cleanup targets a different
   label/identity shape); it is tracked separately and must not be treated as
   a working reference shape for new code. The wider orphan-sweep subsystem
   carried the same negated-pattern shape and was tracked and fixed in #5147.

## Fixes

- `executeShellExecRetractStatements` and `executeDocumentationRetractStatements`
  route through the shared sequential-retract path (edge retract before orphan
  cleanup, each statement in its own auto-commit transaction).
- All `CodeInterprocEvidenceWriter` retracts route through the sequential
  `dispatchRetract`; the MERGE-shaped write keeps its grouped dispatch.
- Both orphan ShellCommand cleanup statements use the COUNT-subquery orphan
  check.

## Benchmark Evidence:

Failing-then-green live regressions on the pinned production backend (behavior
change — the old paths returned wrong graph truth):

```bash
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17692 \
go test ./internal/replay/offlinetier/ -run 'TestReducerContentEdgeRetractGraphTruth|TestCodeInterprocTaintEdgeRetractGraphTruth' -count=1
```

- RED (shell): `retract: in-scope EXECUTES_SHELL gone: count = 1, want 0`.
- RED (taint): `retract: in-scope TAINT_FLOWS_TO gone: count = 1, want 0`.
- RED (cleanup, after the sequencing fix): `retract: orphan ShellCommand
  cleaned: count = 1, want 0` under both the negated-pattern and the
  OPTIONAL MATCH shapes.
- GREEN: ok 3/3 runs (~1.1–1.3s package wall) after all fixes. The content
  test writes and retracts EXECUTES_SHELL (with orphan cleanup and an
  out-of-scope survivor), DOCUMENTS through the delta path (out-of-scope
  survivor), and CORRELATES_DEPLOYABLE_UNIT; the taint test writes and
  retracts TAINT_FLOWS_TO with an out-of-scope scope surviving and all
  Function endpoints intact.

Cost shape: the shell-exec retract is two bounded statements in two
auto-commit transactions instead of one grouped transaction, the documentation
delta retract at most two, and the taint retracts stay single statements — the
same fixed-fan-out class as the previous retract fixes. The prior grouped
paths were buying their single transactions by not deleting edges, and the
prior orphan cleanup did no work at all, so there is no correct faster
baseline to regress against.

## Observability Evidence:

No-Observability-Change. The retract statements keep their operations,
parameters, and the existing canonical retract spans and graph-write
failure/retry telemetry; sequential execution surfaces per-statement errors
through the same `WrapRetryableNeo4jError` path. No metric name, span, log
field, queue stage, worker knob, or status field changes.
