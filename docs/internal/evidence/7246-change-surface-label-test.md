# Change-surface outgoing read: label test before labels() (#7246)

The outgoing impact traversal behind `POST /api/v0/impact/change-surface`,
`/change-surface/investigate` and the MCP `find_change_surface` tool
enumerates every path of up to four hops from the Repository, then ran six
`'X' IN labels(impacted)` terms on each path. On the largest indexed
repository (12,402 files) that is 263,186 paths, the labels() filter was
1,842,302 of 2,251,956 DB hits, and the answer was zero rows. This change adds
a cheap `impacted:Label` conjunct ahead of those terms and names the read in
telemetry. It changes no returned row.

Performance Evidence: query shape `MATCH path = (start:Repository {id:
$target_id})-[*1..4]->(impacted) WHERE impacted.id <> $target_id AND (...)
RETURN ... ORDER BY depth, name, id LIMIT $limit` (`changeSurfaceLegacyCypher`,
unscoped, no environment), backend Neo4j 2026.08.1 (SLOTTED runtime) on the
shared QA graph, anchor `NodeUniqueIndexSeek` on `Repository.id`, the
`repository_id` uniqueness constraint present. Before: six
`'L' IN labels(impacted)` terms. After: `(impacted:A OR impacted:B ...) AND
(the same six IN labels() terms)`. Nine repositories from 70 to 12,402 files,
all with the same row output and the same full path set (table below). The
fixed form was faster on all nine, 3.4x to 4.8x on the median server time and
3.2x to 3.7x on DB hits. No case got slower.

Observability Evidence: the outgoing traversal now reports
`graph_query_name=platform_impact.change_surface.outgoing` (it logged
`unnamed`), on both the `query.graph_read.warning` log and the
`eshu.graph_read.query_name` span attribute. The name is documented in
`docs/public/reference/telemetry/graph-read-safety.md` and pinned by
`TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry`.

## The change

```cypher
WHERE impacted.id <> $target_id
  AND (impacted:Repository OR impacted:Workload OR impacted:WorkloadInstance
    OR impacted:CloudResource OR impacted:TerraformModule OR impacted:DataAsset)
  AND ('Repository' IN labels(impacted) OR 'Workload' IN labels(impacted) ... )
```

On Neo4j 2026.08.1 (SLOTTED) the planner evaluates the label test first
(PROFILE: identical DB hits for the label-test-only and combined forms), so
the node-label check discards every non-whitelisted path before the six
labels() calls run and those calls see no rows when nothing survives. This is
an observation on that version, not a claim about the planner's mechanism. The scoped traversal
(`changeSurfaceScopedOutgoingCypher`) and the repository-consumers read are
byte-unchanged; `TestChangeSurfaceNonLegacyCypherIsPinned` holds their
digests. `changeSurfaceRowLabelAdmitted` and the Go-side filters are
unchanged.

The IN labels() terms stay because NornicDB v1.3.3 ignores a label test in the
WHERE of a relationship MATCH and evaluates only `'Label' IN labels(x)` there
([6786](6786-nornicdb-label-predicates.md)). On that backend the new conjunct
is not enforced and the IN labels() terms still enforce the whitelist.

The #6786 label-predicate guard rejected any label test in that position, so it
now accepts one only in the exact shape proven live on NornicDB and Neo4j
(`labelTestPairExempt`): a plain MATCH that opens its frame, with one
variable-length relationship, one tested variable, and the label test AND-ed
beside an IN labels() disjunction over the same label set, with no top-level
OR and no other label test. Other positions stay rejected because
[6786](6786-nornicdb-label-predicates.md) measures the label test as
evaluated there: the WHERE of a second MATCH returns 0 rows (row H02), an
OPTIONAL MATCH nulls the row (row H05), and a negated test returns 0 rows
(row D04). A positive label test is ignored only in the single-relationship
rows (A02 through I01). The seeded RED/GREEN pair is
`TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct`: 16 RED cases
(including H02, a MATCH after a WITH, H05, a fixed-length relationship, two
relationships and two tested variables) and 2 GREEN cases, both the production
statement text that the live tests run. With the exemption forced true all 16
RED cases fail. With the position and shape gate removed (the earlier,
wider exemption) the H02, H05, WITH-preceded, fixed-length and
two-relationship cases fail.

## Failing test first

`TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest` splits the
rendered WHERE into its top-level conjuncts and requires three: the id guard, a
label test over exactly `changeSurfaceImpactedLabels`, then the IN labels()
guard. On the unchanged code it failed with `unscoped outgoing WHERE has 2
conjuncts, want 3`. `TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry`
failed with `outgoing graph_query_name = "unnamed"`.

## Method

- Statements are the text the Go builder renders, recorded from a throwaway
  test at `origin/main` (before) and at this branch (after), unscoped,
  depth 4, `limit = 11`. The two outgoing statements differ by exactly the
  two added lines; the consumers statement is identical. Tabs became spaces so
  the shell accepts the text.
- Timing: `cypher-shell` in a pty, read-only, `ready ... consumed after`
  summed server time. Each session ran both statements back to back. One warm-up
  session per repository was discarded (it carries plan compilation; ready time
  was 0 to 2 ms for every statement in the measured rounds). Then six sessions
  alternated the first mover, three each way. Neo4j caches plans, not results,
  so every measured run did the full execution.
- DB hits: one `PROFILE` per variant per repository, first mover alternating.
- Equivalence: the same WHERE with the RETURN replaced by one key per path
  (`length|impacted.id|relationship element ids`), read in the order fixed,
  base, fixed, base, and compared as sets both ways. The final 11-row output
  was read in the same four sessions and compared ordered and sorted.
- Host load average on the machine driving the reads was 15 to 41 during the
  timing run, from other work on it. The measured time is server-side and the
  per-repository ranges are tight, so the load did not show up in the
  numbers.

## Results

Server time is `consumed` ms, median (range) of 6. Paths is the pre-LIMIT count
of whitelisted paths. The two zero-path rows are the large repositories where
the whole cost is the filter.

| Repository | Files | Paths | Before ms | After ms | Faster | Before DB hits | After DB hits | Fewer hits | Rows |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| r_8946df89 | 12402 | 0 | 552 (546-574) | 145 (141-151) | 3.80x | 2,251,956 | 672,840 | 3.35x | 0 |
| r_957cd853 (wordpress) | 7096 | 0 | 306 (302-311) | 89 (84-91) | 3.44x | 1,237,476 | 387,996 | 3.19x | 0 |
| r_a09c7db8 | 3906 | 223 | 2172 (2150-2423) | 500 (491-507) | 4.34x | 9,104,374 | 2,540,923 | 3.58x | 11 |
| r_33474efb | 3504 | 171 | 740 (731-769) | 174 (172-179) | 4.24x | 3,330,889 | 922,192 | 3.61x | 11 |
| r_413930a8 | 1499 | 240 | 86 (84-87) | 24 (22-24) | 3.64x | 385,202 | 112,002 | 3.44x | 11 |
| r_20871f7f | 1053 | 48,152 | 3833 (3815-4084) | 825 (819-839) | 4.65x | 19,801,576 | 5,301,721 | 3.73x | 11 |
| r_0a05b11a | 369 | 116,510 | 12444 (12168-16187) | 2618 (2594-2870) | 4.75x | 60,738,803 | 16,233,176 | 3.74x | 11 |
| r_2645123f | 146 | 644 | 88 (81-95) | 20 (19-33) | 4.29x | 266,282 | 76,258 | 3.49x | 11 |
| r_4ff9d0b5 | 70 | 364 | 50 (49-51) | 13 (12-13) | 3.81x | 161,709 | 47,443 | 3.41x | 11 |

Row sets: for all nine repositories, the before and after path sets are equal
in both directions in all four reads, and the 11-row output is identical in
order. A first equivalence pass showed 24,285 extra paths for `r_0a05b11a`
on the after side. That repository was ingesting: the same statement read
84,368, then 108,653, then 116,510 paths over a few minutes. The repeat above
reads each variant twice and the four sets match, so the difference was graph
growth, not the change.

Order effect: running second was not uniformly faster or slower. The largest
gap is `r_2645123f`, where the after statement had a median of 27 ms as first
mover and 20 ms as second (3 runs each, with the 33 ms outlier among the
first-mover runs). Comparing the before and after medians within the same
position (first mover with first mover, second with second) gives 3.2x to 4.8x
across the nine repositories, so the ordering does not explain the gain.

`r_0a05b11a` (369 files, 116,510 paths) took 12.4 s before. That is past the
10 s `DefaultGraphReadTimeout`, so on today's data the shipped statement can
hit the bounded-read deadline for it. After the change it is 2.6 s. The
remaining cost there is the expansion itself, which this change does not
touch.

## NornicDB

The pinned image (`ghcr.io/eshu-hq/nornicdb-amd64-cpu` at
`sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`)
was run locally with the live tag
`live_nornicdb_label_predicates`:

```bash
cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=nornicdb \
  go test ./internal/query/impact -tags live_nornicdb_label_predicates \
  -run 'TestLiveChangeSurfaceLabelPredicate|TestLiveChangeSurfaceLabelConjunctDeepTraversal' -count=1 -v
```

The live seeds use CREATE without cleanup, so each run needs a fresh store
(a unique container, removed afterwards).

`TestLiveChangeSurfaceLabelPredicate` (unscoped and scoped) and the new
`TestLiveChangeSurfaceLabelConjunctDeepTraversal` (whitelisted nodes at one,
two and three hops through File and Function nodes, an environment filter, and
a limit that truncates the whitelisted set) pass on NornicDB and on
`neo4j@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`.
This shows the conjunct does not empty or leak the traversal on v1.3.3. It does
not tell whether NornicDB ignores or evaluates the conjunct; either gives the
same rows.

## Not claimed

- Only Repository anchors were timed. The same statement also serves
  Workload, WorkloadInstance, CloudResource, TerraformModule and DataAsset
  anchors, and none of those were timed in the table above. A path set that is
  fully whitelisted pays about one extra label check per path (the label test is
  one DB hit per path in the PROFILE) and gains nothing, so such an anchor is
  expected to get slightly slower, not faster. That bound is reasoning from
  the PROFILE, not a measurement.
- No deployed latency. The request-time effect needs a rebuilt image replayed
  against the same corpus; only statement server time is measured here.
- The expansion cost is unchanged: every path of up to four hops is still
  enumerated. For `r_8946df89` the remaining 145 ms is that expansion.
- A cheap emptiness pre-check (a pruned BFS before the enumeration) was
  measured at 57 to 66 ms in the diagnosis and is not part of this change.
- The scoped-grant outgoing statement and environment-filtered or non-default
  depth reads were not measured; they are unchanged or not on the measured path.
- One shared graph, one Neo4j version. Other data shapes, or a Neo4j planner
  that reorders the conjuncts, are not covered.
