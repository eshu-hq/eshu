# Generation Tag on content_files (#7760)

The permanent fix for the #7609 ahead-manifest corner. `content_files` gains
`generation_id` (migration 169): the content writer stamps every stored file
row with the materialization's generation id, and the two producer manifest
reads (`PackageManifestsQuery`, `GoModuleManifestsQuery` in
`go/internal/storage/postgres/code/producers/store.go`) bind content to the
generation that wrote it instead of inferring dirt from proxy signals.

The tag rule is fail-safe and status-agnostic: a row reads clean only when
its tag names a generation row with `activated_at IS NOT NULL`. A NULL tag
(legacy or mixed-deploy write), a dangling tag (generation retention-pruned;
there is deliberately no foreign key), an empty tag, or a tag naming a
never-activated generation all read as a dirty NULL row the anchored scan
must visit, never dropped. A tag from an older activated generation is
clean by carry-forward: the active generation never rewrote the path, so the
stored bytes are still the active truth. Do not compare `superseded_at` to
`activated_at`: Ack stamps the prior active with the same clock, which would
dirty every scope with two activations.

The #7776 dirty predicate is partially replaced, not removed: per-row tags
cannot see row-less scopes, so the manifest-less generation leg stays. A
scope with no stored manifest but a never-activated generation still
resolves dirty through that leg (arbiter condition C1). The timestamp and
generation-exists legs over stored manifests are gone; the backfill test
pins the precision gain (an unrelated unactivated generation no longer
dirties a clean scope).

The backfill attributes a legacy row to the scope's active generation only
where all hold: (i) the scope has zero generation rows with `activated_at IS
NULL`, (ii) the active row exists with status `active` and activated, (iii)
the row's `indexed_at` is at or before the activation (per-row guard), (iv)
exactly one repository scope maps the repo key. In the ahead corner the
writing generation has NULL `activated_at`, so (i) excludes exactly the
dangerous scopes; backfilling them to active would bless ahead content as
active truth (the #7609 bug). Post-hole scopes whose signal row was
retention-pruned pass (i) but fail (iii): they stay NULL (dirty), no worse
than the pre-tag read. Scopes failing any clause converge as generations
rewrite their manifests.

Residuals: a manifest deleted by a write with no generation row leaves no
row and no tag, and is uncatchable (second-order: crash-window write plus
ambiguity-relevant load). A behind-skewed writer clock on a
post-retention-hole scope can wrong-clean through clause (iii) exactly as
the timestamp leg this tag replaces could; live unactivated generations are
still caught by the tag regardless of skew.

Migration proof (local PostgreSQL 18, 2026-10-09): 1,000,000-row
`content_files` fixture, 3,000 repository scopes, 1% planted never-activated
generations, 10% of filler rows indexed after the activation. ADD COLUMN:
4.0 ms (catalog-only, no rewrite). Backfill UPDATE: 30.9 s, tagging 890,300
rows and skipping 109,700 (30 pending-scope manifests plus 9,970
pending-scope filler rows by clause i, 99,700 late filler rows by clause
iii; the skip counts cross-check the seed arithmetic exactly). ANALYZE:
1.0 s. The UPDATE takes row locks only (ROW EXCLUSIVE): concurrent reads
proceed, no maintenance window. Re-runs are idempotent via the `WHERE
generation_id IS NULL` guard, which also never clobbers tags the new writer
stamps mid-migration.

Read-side proof on the same fixture, `EXPLAIN (ANALYZE, BUFFERS)` of the
final `PackageManifestsQuery`: 865.8 ms first run (cold cache), then 46.1 ms
and 46.6 ms warm, 13,518 shared-hit buffers, no new index. The manifest CTE
still reads once through `content_files_relative_path_trgm_idx`; the tag leg
is an Index Scan on `scope_generations_pkey` at loops=3000, ~0.002 ms per
probe, confirming the pre-change 100k-row shim. The manifest-less leg is a
1 ms anti-join returning zero rows on this fixture (every scope stores a
manifest). The diagnostic per-outcome count on the fixture reads 2970 clean
plus 30 null_tag: the 30 pending-scope manifests stayed NULL through clause
(i), as designed. The `unactivated_tag` outcome fires only when a tag names
a live never-activated generation and is pinned by the live ahead-write
test, not by this fixture.

Before/after on the same fixture shape (review finding 3): the base-revision
query (the #7776 dirty union) runs 19.6 ms warm on a freshly seeded table,
but that table holds manifests in 53 contiguous heap blocks, which flatters
any scan. After the migration UPDATE churns the table (the realistic layout)
plus `VACUUM (ANALYZE)`, base runs 33.2 ms and 33.4 ms warm with 4,173
buffers for 3030 rows, while the new query runs 36.9 ms and 37.7 ms warm
with ~13,000 buffers for 3000 rows: +11%, bounded by the 3000 pkey probes
(~6 ms, partly offset by dropping the MAX aggregate and IN-subquery leg)
and the wider CTE carrying the tag. All buffers are shared-hit; no new
index. The loader runs this read once per code-call materialization pass,
not per key, so a sub-50 ms read keeps its budget while the tag buys the
precision gain and the post-hole permanence the union could not.

Operator visibility: each manifest read counts every row it saw into
`eshu_dp_producer_manifest_tag_outcomes_total` by `kind` (package|gomod) and
`outcome` (clean|null_tag|dangling_tag|unactivated_tag|manifest_less). A
rising non-clean share after deploy means tags are failing safe; the
per-outcome diagnostic above distinguishes legacy NULLs (converging as
generations rewrite) from live unactivated tags (real ahead-write
pressure). Post-deploy SQL: wrap either manifest query as
`SELECT outcome, count(*) FROM (<query>) AS q(scope_id, content, outcome)
GROUP BY 1 ORDER BY 1;`.
