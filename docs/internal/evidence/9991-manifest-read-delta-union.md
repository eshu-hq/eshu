# #9991 Manifest Dependency Read: Delta-Active Scopes Keep Their Base Pins

`FactStore.ListActivePackageManifestDependencyFacts` joined
`fact.generation_id = scope.active_generation_id`. A git delta generation
stores only its changed files' rows, so a delta-active repository returned only
the delta's pins and lost every unchanged pin of its base full generation. The
#7797 overlay ran only at projector load time. Reproduced on PostgreSQL 18.6:
a full with `alpha` and `bravo`, then an active delta that re-emits `alpha`,
loaded `[delta-alpha]`.

## Change

`go/internal/storage/postgres/active_package_manifest_dependency_delta.go`:

1. The active query pages unchanged.
2. A base-candidate query, derived from the active text by
   `deltaBasePackageManifestDependencyQuery` (two fragment swaps, fail-closed
   shape check), pages through the same partial indexes, keyset cursor, and
   limit. It selects activated full generations at the delta's union-base
   commit, `COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha)`,
   a strict superset of `deltaUnionBaseQuery`.
3. One batched lookup (`manifestDeltaTargetsQuery`) names the delta-active
   scopes among the rows. For each one, `resolveDeltaBaseAtCommit` picks the
   base the projector overlays, including the retention prune probe and the
   refusal.
4. One batched read (`manifestDeltaOverlayInputsQuery`) loads each delta's
   repository fact and its stable-key collisions, tombstones included. Then
   `facts.OverlayDeltaBase` keeps the base rows the delta did not override,
   tombstone, or touch.
5. All statements share one read-only repeatable-read snapshot. Inside the
   impact snapshot the transaction nests as a no-op.

A refused base, a legacy delta with no baseline, or a delta with no
`delta_generation` repository fact keeps today's delta-only rows. A lookup
error fails the read.

Rejected: one combined statement, active OR base generation per row. It read
the same 21,205 rows at a 6.16 s median against 4.47 s for the two paged
statements on the same data. A SQL copy of the base rule and the overlay was
also rejected: it would be a second definition of #7797.

## Performance Evidence:

Source `95fa47b4be2f` (branch `lane3/9991-delta-manifest-read`). Disposable
PostgreSQL 18.6 container, image
`sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`
(`postgres:18`, default settings), on a shared developer host. The schema is
`ApplyBootstrap`. The corpus has 2,001 git scopes. 2,000 of them carry 25 npm
pins in each of a superseded and a current full (100,000 rows), and 200 of
those are delta-active with a 5-row delta. The `big` scope has a 50,000-pin
base over 500 manifests and an active 5,000-row delta over 50 of them. That is
156,201 fact_records in total, with 400 names in `$2`. The harness was a
throwaway, uncommitted test calling the production code over `SQLDB`. "Old"
pages the shipped active query only, the statements the read sent before
#9991. "New" is the whole `ListActivePackageManifestDependencyFacts`. Each
figure is the median of seven runs.

| shape | old rows | old median | new rows | new median |
| --- | --- | --- | --- | --- |
| npm, 201 delta-active scopes | 11,172 | 1.513 s | 20,000 | 4.518 s |
| npm, same corpus with every full active (no delta) | 20,000 | 4.116 s | 20,000 | 4.322 s |
| maven, no matching rows | 0 | 9.2 ms | 0 | 22.9 ms |

The first row is not a like-for-like comparison: the old read returned the
wrong, short row set. The 8,828 extra rows match an independent SQL count of
the base pins the delta did not override or touch (11,172 + 8,828 = 20,000).
The comparable figures:

- The same 20,000 pins read through a delta-active corpus cost 4.52 s,
  against 4.12 s read from active fulls (+9.8 %).
- With no delta-active scope, the fixed overhead is one extra base page, the
  target lookup, and BEGIN/COMMIT: +0.21 s (+5 %) here, and +14 ms (mostly
  planning) for an empty read.

`EXPLAIN (ANALYZE, BUFFERS)`, page 1, delta-active shape:

| statement | time | buffers | plan |
| --- | --- | --- | --- |
| active page (npm) | 212.8 ms | 284,936 | `fact_records_active_package_dependency_entity_idx`, nested loop, top-N sort |
| base page (npm) | 278.0 ms | 401,513 | same index, same shape, plus the `active_delta` probe by `scope_generations_active_scope_idx` |
| active page (maven) | 0.13 ms | 9 | same index, sbt branch |
| base page (maven) | 0.16 ms | 9 | same index, sbt branch |
| delta targets, 2,001 scope ids | 1.69 ms | 93 | primary keys |
| overlay inputs, 201 pairs, 15,000 keys | 4.16 ms | 2,119 | `fact_records_scope_generation_idx` |
| base lookup, one scope | 0.05 ms | 8 | `scope_generations_scope_idx` (unchanged #7797 query) |
| prune probe, one scope | 0.008 ms | 0 | `generation_retention_events_generation_idx` |

No statement runs a sequential scan. The base page has the active page's
shape. Each page costs one index-prefix scan, the same as the active read's
existing paging (#8961). The next measured bottleneck is that per-page prefix
scan, which predates this change. Base resolution runs once per delta-active
scope that has base or delta pins, two statements for a superseded base; 201
scopes added about 50 ms in total.

## Observability Evidence:

- Counter `eshu_dp_package_manifest_dependency_read_delta_union_total` by
  `read` and the closed `outcome` set: `union_base_found`,
  `union_base_unmatched`, `union_base_missing`, `union_scope_mismatch`,
  `union_no_baseline`, `union_resolve_failed`. No scope, generation, or
  commit labels.
- One `package_manifest_dependency_read.delta_union` span event per decision,
  and one WARN log for `union_base_missing` and `union_scope_mismatch` with the
  scope, generation, both commits, and `chained`.
- The existing read histograms keep their meaning. `read_pages` counts active
  pages only, so the base read's cost shows as wall time while pages hold.

## Proof

- `go test ./internal/storage/postgres -run 'TestListActivePackageManifestDependencyFacts(Delta|Chained|FullActive)' -count=1`
  against the disposable DSN runs nine live cases. All but the full-active
  guard failed before the fix and pass after it.
- Mutations: dropping tombstones from the overlay inputs fails the tombstone
  case. Resolving at the baseline alone instead of the union base fails the
  chained case.

## Readers with the same active-generation-only join

These join `scope.active_generation_id = fact.generation_id` over git-collected
facts, so a delta-active repository shows them delta-only rows. They are not
fixed here.

- `facts_active_code_call_symbols.go:47` (`file`)
- `facts_active_jvm_reachability.go:43` (git `file`)
- `facts_active_crossplane_xrd.go:46` (git `content_entity`)
- `owned_package_targets.go:173` (git `content_entity`)
- `facts_active_cicd_workflow_image.go:42` (git `ci.workflow_image_evidence`)
- `eshu_search_document.go:186`, `eshu_search_vector_pending.go:42`,
  `code_drifted_findings.go:32`, `terraform_config_state_drift_findings.go:273`
  (the fact kind is a parameter; affected when it names a git kind)
- `facts_active_package_ownership.go:36`,
  `facts_active_security_alert_reconciliation.go:36`,
  `facts_active_sbom_attestation_attachment.go:38` (fact-kind lists; affected
  for any git kind they include)

`facts_active_repository.go:54` reads the `repository` fact, which every delta
carries. The other active readers join non-git sources (cloud, OCI, CI runs,
incidents) or `reducer_*` facts. Git delta generations do not apply to them.
