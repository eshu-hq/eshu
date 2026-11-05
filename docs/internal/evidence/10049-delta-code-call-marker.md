# #10049: Code-Call Marker on Delta Generations

A git delta that moves a function gives it a new content-entity id (the id
hashes the line number), and the git fact builder returned before the
`code_call_materialization` follow-up on delta generations, so nothing
rewrote the moved caller's CALLS edges until the next full generation. The
marker now fires on delta generations (streamed before the delta early
return in `fact_builder.go`), with the delta pre-count bumped by one. Two
prerequisites make it safe and are already on main: the symbol-definition
read overlays the delta on the active generation (#10003 row 1, #10124), so
callees in unchanged files still resolve under the file-scoped refresh; and
an unusable delta scope fails the work item closed (#10023, #10114) instead
of falling back to the whole-repository retract.

The handler's three symbol-to-runtime domains (`handles_route`, `runs_in`,
`invokes_cloud_action`) retract repo-wide and have no file-scoped retract.
Pairing their repo-wide refresh with a delta's changed-file rows would
delete every unchanged file's runtime edges, so the handler gates them off
delta input entirely (`runtimeDomainEnvelopes` in
`materialization/refresh.go`): a delta repository emits no per-edge rows
and no refresh for those domains, exactly as before the marker fired on
deltas, and its runtime edges refresh at the next full generation. The
`code_calls` side of the same pass owns a file-scoped retract and does run.

Proof, all hermetic in this change:

- `TestBuildStreamingGenerationDeltaEmitsWholeRepoResolveFollowups` (RED
  first): a delta generation emits exactly one `code_call_materialization`
  follow-up.
- `TestCodeCallMaterializationHandlerDeltaMovedCallerKeepsUnchangedCalleeEdge`:
  a caller in the changed file keeps its CALLS edge to a callee defined
  only in an unchanged file (supplied through the active symbol-definition
  load), under a file-scoped refresh over the changed file only.
- `TestCodeCallMaterializationHandlerDeltaGatesRuntimeDomains` (RED first):
  a delta emits zero rows in all three runtime domains; the full control
  emits per-edge rows plus one refresh per domain.
- `TestCodeCallProjectionRunnerFileAndWholeRefreshInterleaveDrains`: a
  file-scoped and a whole-scope refresh pending for one repository in one
  acceptance unit — the fenced shape cannot retract, write, or mark on an
  adversarial first partition attempt, the leading shape always progresses,
  and both creation orders drain with each row marked once and the edge
  written once. Conflict domain: `code_calls` intent partitions per
  repository, ordered by the existing refresh fence and partition leases;
  no new lock, queue, or serialization was added.
- B-12: the dr-5 `expected_failure` marker is removed (the moved-caller
  edge is now a plain required edge) and `code_call_symbols` leaves
  `unexpressed_readers`, because dr-1 to dr-3 and dr-5 express it.

## No-Regression Evidence:

Full generations are unchanged apart from follow-up stream order (the
marker moved ahead of the delta return, so it streams before the
full-only follow-ups); fact kinds, payloads, and counts per generation are
identical, and `TestGenerationContentVersionPinsCollectorOutput` passes
with no epoch re-pin — no fixture file's attributed output moved. The new
work is bounded per delta: one additional work item whose handler loads
the delta generation's changed-file facts and the referenced symbol keys
(the #10124 overlay measured that read at a 1.25x counterfactual against
the all-full read, with the base scan only for reached delta scopes). The
runtime-envelope filter is one early-out pass over the already-loaded
envelopes (no pass at all when no repository is on a delta). A delta with
an unusable scope dead-letters on attempt 1 before the symbol load, so a
bad delta costs no symbol query and no graph write. End-to-end delta
wall time is NOT_CHECKED here: the live delta-reader leg runs at promotion
(the shared host was under another lane's live proof during this change).

## Observability Evidence:

No new instrument. The delta path now exercises the existing signals: the
handler completion log (`code call materialization completed` with
`file_scoped_repo_count` and the full-refresh fallback counts), the
`eshu_dp_code_call_delta_scope_unusable_total{reason}` counter and its
ERROR log for fenced deltas, the overlay outcome counter
`eshu_dp_code_call_symbol_definition_read_delta_union_total`, and the
standard reducer execution and code-call edge batch telemetry. Docs that
called those signals dormant "until #10049" now describe them as live.
