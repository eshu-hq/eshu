# Go module repositories

## Purpose

`gomodules` reads one rotation page of repositories for the Go reachability
scanner: their canonical id, active-generation commit, and stored remote. It keeps
a repository whose stored content holds a `go.mod`, or whose go-reachability scope
still holds Go evidence. It also retracts Go evidence the commit-skew rule names, and
reopens the scopes holding findings for that repository.

## Ownership boundary

This package owns one read and one write. The coordinator root owns the schedule and
admission, and `coordinator/reachability` owns planning and the skew rule. It needs no
migration.

## Exported surface

- `Store`, `NewStore`, `Store.ListGoModuleRepositories`, `Store.WithHolderReopener`,
  `Store.FenceStaleGoEvidence`, and `HolderReopener`.

## Dependencies

`collector/scannercomponent` for the value types, `facts` for the finding fact kind, and
`storage/postgres/db` for the queryer. The composition root binds `HolderReopener` to
`postgres.ReopenSupplyChainImpactHolderInTransaction`.

## Telemetry

None of its own. The coordinator wraps the queryer so the read and the fence report
on `eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}`, and
records `eshu_dp_workflow_coordinator_activation_derived_scopes_fenced_total` from the
count the fence returns.

## Gotchas / invariants

- Where the data lives: the remote is `ingestion_scopes.payload->>'remote_url'`,
  written by the git collector's `buildScope` in normalized form. The repository id
  is `ingestion_scopes.source_key`, and the commit is
  `scope_generations.source_commit_sha`.
- A repeat pass in a bucket is one primary-key probe on `workflow_runs` once the
  bucket's run exists. A page that plans no run creates none, because it has no
  plannable repository or every target is still open, so the full page query repeats
  on every reconcile tick for that bucket: about 98 ms warm in the measurement below.
  That is deliberate: it admits a target as soon as it closes.
- A repository is planned on `has_go_module OR has_go_evidence`. Without the second
  branch, a repository that lost its `go.mod` is never claimed, so its Go evidence
  never retracts. `has_go_evidence` is true while the repository's
  `go-reachability://` scope has an active generation holding a non-tombstoned
  `vulnerability.go_module_evidence` or `vulnerability.go_call_reachability` fact. The
  component scans once more and lands a trusted `no_go_modules` generation holding
  only a warning, so the repository drops out on its next visit. That retracts facts,
  not stored findings, which keep their verdict until a reducer pass re-derives them
  (eshu-hq/eshu#10077). The cost is one extra shallow clone per lost `go.mod`.
- The evidence probe is the `ingestion_scopes` primary key plus a prefix of
  `fact_records_scope_generation_idx (scope_id, generation_id, fact_kind)`, and it
  adds about 2 ms to a page. Same run, warm, one 256-candidate page: 95.4 and 96.2 ms
  before, 97.3 and 98.2 ms after; 128,393 buffers before, 129,543 after. Cold it took
  153.2 ms. `fact_kind` is in the index condition, so 200,000 warnings in an active
  generation are not scanned: all 72 fact probes together cost 310 buffers. Compare
  within this run only: the 68 ms above is a different run, whose `content_files`
  scan made no heap fetches, where this one made 603,551.
- Those rules are held by the live suite in `store_live_test.go` (6 live tests) and
  by mutations of `has_go_evidence`: each branch of the `OR`, each evidence kind,
  warnings counted as evidence, any-generation reads, and tombstones. Evidence:
  `docs/internal/evidence/10027-go-modules-derivation.md`.
- `HasGoEvidence` is set on every returned row with evidence, so the planner can raise
  its invariant alarm for a non-canonical remote that holds evidence. A dead remote is
  not this case: it is planned, and its claim fails (eshu-hq/eshu#10055).
- The page also returns the go-reachability scope's active generation and commit, and, only
  for a skewed row, `evidence_left_at`: the newest `superseded_at` among the repository's own
  generations at that commit. `reachability.StaleEvidence` applies the rule, and
  `FenceStaleGoEvidence` writes the verdict.
- A fence takes the scope row, supersedes the generation, finds the holders on
  `fact_records_supply_chain_impact_repository_lookup_idx`, and reopens each, in one
  transaction with a 2s `lock_timeout`. A successor's ack in either order ends on the
  successor. A fence that loses the race changes nothing and counts nothing. Evidence:
  `docs/internal/evidence/10055-commit-skew-fence.md`.
- A repository scope that retires leaves its go-reachability scope active, because
  candidates are active repository scopes only. Core lifecycle owns that, not this
  query (eshu-hq/eshu#10054).
- Detection reads `content_files`, not the `module_declaration` fact. For one
  256-candidate page over 1.84M facts, the fact probe took 1,511 ms and read 197k
  buffers; this probe took 68 ms and read 7.5k. Do not swap in the fact probe
  without an index for it: a repository with no such row is scanned to its end.
- That choice can disagree with the fact in two cases, and
  `TestListGoModuleRepositoriesDetectsFromContentNotFactsLive` pins the source:
  1. A `go.mod` with no `module` line is planned, though the parser emits no fact.
     The component reads the commit and finds no module.
  2. Content ahead of or behind the active generation. A stale row causes one empty
     scan; a `go.mod` whose content has not landed waits one rotation.
- Measured on Postgres 16 for one 256-candidate page, with 3,000 repositories,
  100,000 other scopes, 1.84M facts, and 820,000 content files: 66 ms cold, 55 ms warm.

## Related docs

- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
