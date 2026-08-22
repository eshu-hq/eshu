# #5351 Ifá materialized-edge coverage — roadmap / known gaps

Moved out of `specs/ifa-materialized-edge-coverage.v1.yaml`'s trailing comment
block to keep that manifest under the repository's 500-line policy cap; the
manifest itself carries only a short pointer to this file. Nothing here is a
`coverage:` or `waivers:` row — this is prose, discoverable from the gate
itself per the manifest's own claims-ledger rule, not only from a PR body.

These are UNCLAIMED dimensions per that rule: a maintainer reads anything
listed here as unproven unless the manifest's own `coverage:` rows say
otherwise. They are recorded so the honest gaps travel with the gate rather
than living only in a PR description.

- sql_relationships DELTA-LIVE is proven by the delta_tombstone row above:
  the ifa-determinism matrix drives gen 1, asserts its exact nine-edge set,
  drives gen 2 with the reused source_run_id, drains again, and asserts the
  accumulated exact nine-edge set with INDEXES retargeted to orders.
- sql_relationships FAULT is covered. #5555 rebuilt both cells
  (scripts/lib/ifa_fault_injection_sql_cells.sh) so they target the
  sql_relationship_materialization / sql_relationships work item
  specifically (a domain-scoped claimed-row precondition and a SQL edge
  MERGE anchor, not CloudResource). cell_killworker_sql and
  cell_failgraphwrite_sql are green live, so the family carries no waiver.
- code_calls BASELINE and FAULT are covered by the exact-set replay and two
  domain-scoped recovery cells described in the manifest (#5991).
- documentation_edges BASELINE and FAULT are covered by exact three-edge
  assertions and two domain-scoped recovery cells (#5994).
- rationale_edges BASELINE and FAULT are covered by full-record assertions
  in both matrices. The determinism matrix also proves its generation-2
  exact-one survivor; that delta behavior is not a separate required row.
- deployable_unit_edges BASELINE and FAULT are covered once its vacuity
  guard is dispatched and its Odù cataloged (#5993/#6158).
- workload_dependency BASELINE and FAULT are covered by its Odù and
  expected-edge-set fixture (#6003).
- handles_route, runs_in, and invokes_cloud_action BASELINE and FAULT are
  covered by exact-set edge assertions (2/2/1 edges respectively),
  reproduced after a domain-scoped graph-write-failure recovery cell
  anchored at each family's own MERGE template, marker-asserted, drained
  with zero dead letters, and digest-matched against the shared trio
  baseline (#5995/#6000/#5997). That is what this fault coverage claims.
  It does NOT claim mid-pipeline kill/reclaim: any handler-stage
  kill-worker cell for these three would reuse code_calls' own wait_key
  (TestIfaFamilyRegistryHandlerWaitKeysAreExclusive) and prove nothing
  new, so closing this gap needs its own blocker mechanism (a
  runner_lease_hold on the production lease key) -- tracked as a NAMED,
  tracked gap, #6208, not a silent absence. Unlike every family above,
  these three families' edges are written through the SHARED RUNNER
  CYCLE rather than fact_work_items, so their graph-write cells are the
  only live proof of recovery through that path -- a strength of this
  coverage, not a gap. See
  `docs/internal/evidence/5995-5997-6000-symbol-runtime-lock-theory.md`
  for the blocker-mechanism theory-proof behind that follow-up.
- No allProjectionDomains family carries a waiver as of the change that
  retired the trio's waivers: the epic #5344 umbrella #5543, decomposed
  into per-domain child issues #5991-#6003, is complete.
