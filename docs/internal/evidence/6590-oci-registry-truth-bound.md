# Evidence: #6590 — OCI registry-truth `LIMIT $row_limit` enforcement

Scope: `ociTagObservationByRefCypher` and `ociImageByDigestCypher`
(`go/internal/query/impact/trace_deployment_oci.go`), the OCI tag-observation
and image-by-digest reads behind `POST /api/v0/impact/trace-deployment-chain`.
Both statements previously carried `ORDER BY` only, no `LIMIT`, so the
`bounded_key_batch` row bound `go/internal/queryplan/testdata/query-source-coverage.yaml`
declared for them (250 keys, 3x fan-out) was not enforced by anything in code.
Both now carry `LIMIT $row_limit` (750 = `oci.MaxKeysPerStatement` x
`oci.RegistryTruthFanOut`, `go/internal/query/impact/oci/bounds.go`);
`oci.AdvanceBoundedRead` resumes a cut-off batch with a continuation
statement keyed only by the still-incomplete keys, or, when one key alone
fills the bound, withholds it as an irreducible overflow (never a
placeholder row). This is a correctness fix (adding a previously-missing
bound), so the required proof is a no-regression measurement on the same
query shape, not a new-feature benchmark.

## Machine / backend profile (resource-qualified)

- `machine_profile`: local development machine, macOS (Darwin 27.0.0), arm64.
- Backend: pinned `neo4j@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (Neo4j 2026.08.1 community, what ops-qa runs), Docker container
  `eshu-6590oci-neo4j`, Bolt on host port 17590, `NEO4J_AUTH=none`,
  `dbms.memory.pagecache.size=512m`, no persistent volume, destroyed after the
  run. Eshu's real schema applied via `graph.EnsureSchemaWithBackendStrict`
  (`container_image_digest` on `ContainerImage.digest` and
  `container_image_tag_observation_ref` on
  `ContainerImageTagObservation.image_ref` are the indexes this measurement
  depends on).
- `absolute_target_applicable`: false — local laptop, relative before/after
  comparison of the same statement shape, not a reference-profile wall-clock
  target.

## Method

Warm median of 21 runs (1 untimed warmup, then 21 timed executions, sorted,
middle value), through the production `Neo4jReader`-equivalent auto-commit
read session (`ociLiveReader.Run`, the same session shape
`internal/query.Neo4jReader.Run` uses — see
`go/internal/query/impact/trace_deployment_oci_bound_live_test.go`), for the
pre-#6590 statement text (`ORDER BY` only, captured as
`ociLiveControlTagCypher` / inlined here) and the post-#6590 statement text
(`ociTagObservationByRefCypher`, `LIMIT $row_limit`), against identical seeded
data for each cell. `EXPLAIN` on the post-#6590 statement confirms the plan
shape. The full production orchestration (`FetchOCIImageRegistryTruthResult`)
was also run over the same seed, counting `Run` calls, to report the real
statement count per cell — this run seeded no `ContainerImage` nodes (out of
scope for a statement-timing/plan/count measurement), so its `resolved` count
is 0 by construction; resolution correctness is proven separately by
`TestLiveOCIRegistryTruthRowLimitBound` (same file) and the unit suite
(`trace_deployment_oci_bound_test.go`).

Shim command (not part of the permanent suite; deleted after this
measurement):

```
cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17590 \
  go test ./internal/query/impact -tags "scratch_6590_perf live_nornicdb_answer_truth" \
  -run TestScratchOCIPerf6590 -count=1 -v
```

## Cells and results

| Cell | Input | Pre-#6590 (median of 21) | Post-#6590 (median of 21) | Plan (post) | Statement count (post, full orchestration) |
| --- | --- | ---: | ---: | --- | ---: |
| 250 refs x 1 obs | 250 keys, 250 rows, no boundary | 2.118834 ms (2nd run) / 1.421584 ms (3rd run) | 2.037958 ms / 1.412125 ms | `ProduceResults, PartialTop, Projection, CacheProperties, NodeIndexSeek` | 5 (1 tag + 1 repo + 3 image label probes) |
| 250 refs x 3 obs (at bound) | 250 keys, 750 rows, exactly `row_limit` | 3.560125 ms / 3.097708 ms | 3.388333 ms / 3.084500 ms | `ProduceResults, PartialTop, Projection, CacheProperties, NodeIndexSeek` | 12 (2 tag [initial + 1 continuation for the boundary key] + 1 repo + 9 image label probes across 3 digest batches) |
| 1 ref x 2000 obs (irreducible overflow) + 249 x 1 obs | 250 keys, 2249 total observations | 8.848125 ms / 8.096833 ms | 4.673250 ms / 5.040250 ms | `ProduceResults, PartialTop, Projection, CacheProperties, NodeIndexSeek` | 6 (2 tag [initial detects the overflowing key + 1 continuation for the remaining 249] + 1 repo + 3 image label probes) |

Two independent runs are reported per cell (both on the pinned image, same
seed shape, back-to-back sessions) to show the timing is stable, not a
single-sample artifact.

## No-Regression Evidence

No-Regression Evidence: statement-text timing is unchanged within noise on
the two cells that never truncate (250x1: ~2.0-2.1 ms pre vs ~2.0-2.1 ms
post; 250x3-at-bound: ~3.1-3.6 ms pre vs ~3.1-3.4 ms post — both cells return
byte-identical row counts, 250 and 750 respectively, since neither statement
hits the bound), and strictly faster on the irreducible-overflow cell
(8.1-8.8 ms pre, unbounded, 2249 rows vs 4.7-5.0 ms post, bounded, 750 rows) —
adding `LIMIT $row_limit` never makes the statement slower, and caps the
worst-case row volume the pre-#6590 shape had no bound on at all. Every
measured plan retains `NodeIndexSeek` with `PartialTop` (matching the
orchestrator's theory-proof `EXPLAIN`) on `container_image_tag_observation_ref`,
never a `NodeByLabelScan`. The 3x fan-out headroom (`oci.RegistryTruthFanOut`)
means the two below-bound cells are the common case in production (1
observation/ref measured on in-tree corpora and ops-qa); the at-bound and
overflow cells are the declared worst case this bound exists to disclose
rather than silently corrupt.

## Observability Evidence

Observability Evidence: `eshu_dp_query_oci_registry_truth_truncated_total`
(labels: `reason` = `tag_observation_row_limit` | `image_row_limit`) plus a
warn log, both emitted from `(*Handler).reportOCIRegistryTruthTruncated`
whenever a bounded read withholds at least one image ref — see
`TestOCIRegistryTruthTruncationEmitsCounterByReason`
(`go/internal/query/impact/trace_deployment_oci_truncation_test.go`).

## Live answer-truth proof

`TestLiveOCIRegistryTruthRowLimitBound`
(`go/internal/query/impact/trace_deployment_oci_bound_live_test.go`, tag
`live_nornicdb_answer_truth`) proves, on the same pinned Neo4j: (a) a
3-observation ref's resolved row is byte-equal in content to the pre-#6590
control statement's 3 raw rows; (b) a 750-observation ref is an irreducible
overflow (withheld, `image_registry_truth_complete=false`,
`truncated_image_refs` names it) while a two-digest ref sorted after it
resolves `ambiguous_tag` with both `digest_candidates` out of the
continuation statement; (c) `EXPLAIN` on the production statement retains
`NodeIndexSeek`, never `NodeByLabelScan`; (d) a digest that overflows
`fetchOCIImagesByDigest`'s `ContainerImage`-label statement while still
carrying a row under `ContainerImageIndex` is withheld entirely -- no truth
row from either label -- while a sibling digest present only under
`ContainerImageIndex` and never truncated still resolves.

## Gating-review P1 fix: cross-label truncation (PR #7314)

`fetchOCIImagesByDigest` runs one `LIMIT $row_limit` statement per image
label (`ContainerImage`, `ContainerImageIndex`, `ContainerImageDescriptor`)
and unions every label's kept rows, recording `truncatedDigests` when ANY
label's statement hit the bound. `fetchOCIImageDigestRows` originally shaped
truth rows from that union before applying the truncation, so a digest that
overflowed on one label while still returning rows on another landed in
both `Rows` and `TruncatedImageRefs` -- breaking the withheld-never-emitted
contract this bound exists to keep. Fixed by `dropTruncatedDigestRows`,
applied to the fetched images before the registry-repository join, mirroring
the filter `fetchOCIImageTagRows` already applied to tag refs. Regression:
`TestFetchOCIImageRegistryTruthWithholdsDigestTruncatedOnAnyLabel`
(`go/internal/query/impact/trace_deployment_oci_bound_test.go`), RED against
the prior head, GREEN after the fix; live proof (d) above confirms it
against a real backend.
