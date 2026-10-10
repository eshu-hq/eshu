# Change-surface outgoing read: label test before labels() (#7246)

The outgoing impact traversal behind `POST /api/v0/impact/change-surface`,
`/change-surface/investigate` and the MCP `find_change_surface` tool
enumerates every path of up to four hops from the Repository, then ran six
`'X' IN labels(impacted)` terms on each path. On the largest indexed
repository (12,402 files) that is 263,186 paths, the labels() filter was
1,842,302 of 2,251,956 DB hits, and the answer was zero rows. This change adds
a cheap `impacted:Label` conjunct ahead of those terms and names the read in
telemetry. It changes no returned row.

`repo-A`, `repo-B`, `repo-G`, `repo-H`, `repo-J`, `repo-M`, and `repo-O` to
`repo-Q` in this note are stable one-to-one placeholders for the measured
repository ids; the mapping is held outside the repository.

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
3.2x to 3.7x on DB hits. No Repository case got slower. Other anchor classes are
in a separate section: WorkloadInstance anchors gain about 4x, and a fully
whitelisted CloudResource anchor does about 7% more DB hits.

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
(`labelTestPairExempt`): a plain MATCH that opens the top-level frame (not a
CALL or EXISTS subquery), with one variable-length relationship, no second
relationship token and no comma pattern, one tested variable, and the label test AND-ed
beside an IN labels() disjunction over the same label set, with no top-level
OR and no other label test. Other positions stay rejected because
[6786](6786-nornicdb-label-predicates.md) measures the label test as
evaluated there: the WHERE of a second MATCH returns 0 rows (row H02), an
OPTIONAL MATCH nulls the row (row H05), and a negated test returns 0 rows
(row D04). A positive label test is ignored, or evaluated correctly, only in
a relationship MATCH that opens its frame (rows A02 through I01). The seeded RED/GREEN pair is
`TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct`: 21 RED cases
(including H02, a MATCH after a WITH, H05, a fixed-length relationship, two
relationships, a bracketless second relationship, a leading relationship, a
comma pattern, a CALL subquery and an EXISTS subquery over a bound variable,
and two tested variables) and 2 GREEN cases, both the production statement
text that the live tests run. With the exemption forced true all 21 RED cases
fail. With the position and shape gate removed (the earlier,
wider exemption) the H02, H05, WITH-preceded, fixed-length and
two-relationship cases fail. With the top-level-frame, relationship-token and
comma checks removed, the bracketless second relationship, leading
relationship, comma pattern, CALL and EXISTS cases fail.

## Failing test first

`TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest` splits the
rendered WHERE into its top-level conjuncts and requires three: the id guard, a
label test over exactly `changeSurfaceImpactedLabels`, then the IN labels()
guard. On the unchanged code it failed with `unscoped outgoing WHERE has 2
conjuncts, want 3`. `TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry`
failed with `outgoing graph_query_name = "unnamed"`.

Two further pins close review threads on the PR.
`TestChangeSurfaceRepositoryTargetNamesOnlyTheOutgoingTraversal` runs a
Repository target, which is the only target that also runs the
dependency-consumer read, splits the two reads by direction
(`<-[:DEPENDS_ON`), and asserts the outgoing read carries
`platform_impact.change_surface.outgoing` while the consumer read keeps the
unnamed default. Naming the consumer context in a mutation run fails it.
`TestChangeSurfaceEnvironmentScopedCypherKeepsLabelTestFirst` renders the
statement with the environment clause appended and requires four conjuncts in
order: the id guard, the label test, the IN labels() guard, then the
environment predicate. Moving the environment clause ahead of the label test
in a mutation run fails it.

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
| repo-B | 12402 | 0 | 552 (546-574) | 145 (141-151) | 3.80x | 2,251,956 | 672,840 | 3.35x | 0 |
| repo-A (wordpress) | 7096 | 0 | 306 (302-311) | 89 (84-91) | 3.44x | 1,237,476 | 387,996 | 3.19x | 0 |
| repo-M | 3906 | 223 | 2172 (2150-2423) | 500 (491-507) | 4.34x | 9,104,374 | 2,540,923 | 3.58x | 11 |
| repo-J | 3504 | 171 | 740 (731-769) | 174 (172-179) | 4.24x | 3,330,889 | 922,192 | 3.61x | 11 |
| repo-O | 1499 | 240 | 86 (84-87) | 24 (22-24) | 3.64x | 385,202 | 112,002 | 3.44x | 11 |
| repo-P | 1053 | 48,152 | 3833 (3815-4084) | 825 (819-839) | 4.65x | 19,801,576 | 5,301,721 | 3.73x | 11 |
| repo-Q | 369 | 116,510 | 12444 (12168-16187) | 2618 (2594-2870) | 4.75x | 60,738,803 | 16,233,176 | 3.74x | 11 |
| repo-H | 146 | 644 | 88 (81-95) | 20 (19-33) | 4.29x | 266,282 | 76,258 | 3.49x | 11 |
| repo-G | 70 | 364 | 50 (49-51) | 13 (12-13) | 3.81x | 161,709 | 47,443 | 3.41x | 11 |

Row sets: for all nine repositories, the before and after path sets are equal
in both directions in all four reads, and the 11-row output is identical in
order. A first equivalence pass showed 24,285 extra paths for `repo-Q`
on the after side. That repository was ingesting: the same statement read
84,368, then 108,653, then 116,510 paths over a few minutes. The repeat above
reads each variant twice and the four sets match, so the difference was graph
growth, not the change.

Order effect: running second was not uniformly faster or slower. The largest
gap is `repo-H`, where the after statement had a median of 27 ms as first
mover and 20 ms as second (3 runs each, with the 33 ms outlier among the
first-mover runs). Comparing the before and after medians within the same
position (first mover with first mover, second with second) gives 3.2x to 4.8x
across the nine repositories, so the ordering does not explain the gain.

`repo-Q` (369 files, 116,510 paths) took 12.4 s before. That is past the
10 s `DefaultGraphReadTimeout`, so on today's data the shipped statement can
hit the bounded-read deadline for it. After the change it is 2.6 s. The
remaining cost there is the expansion itself, which this change does not
touch.

## Other anchor classes

The same statement serves Workload, WorkloadInstance, CloudResource,
TerraformModule and DataAsset anchors. The table above is Repository only, so
these were run afterwards with the same method: the Go-builder text at base and
at the branch with only the anchor pattern replaced, one discarded warm-up and
six interleaved sessions per case with the first mover alternating, four-read
path-set equivalence (fixed, base, fixed, base) and one `PROFILE` per variant.
Anchors were the highest path counts found on the corpus for each class.
Instance, resource and workload ids are not recorded here.

| Anchor | All paths (pre-LIMIT) | Whitelisted paths | Before ms med (range) | After ms med (range) | Before DB hits | After DB hits | Rows |
| --- | --- | --- | --- | --- | --- | --- | --- |
| WorkloadInstance wi_1 | 401,726 | 19,744 | 1580.5 (1567-1670) | 352.0 (342-452) | 7,585,201 | 2,027,663 | 11 |
| WorkloadInstance wi_2 | 386,324 | 19,171 | 1943.0 (1540-2351) | 498.5 (335-553) | 7,328,877 | 1,956,322 | 11 |
| CloudResource cr_1 | 76 | 76 | 1 (0-2) | 1 (0-1) | 1,045 | 1,126 | 11 |
| CloudResource cr_2 | 61 | 61 | 1 (0-1) | 1 (0-1) | 946 | 1,011 | 11 |
| Workload wk_1 | 137 | 0 | 1 (0-1) | 0 (0-1) | 1,236 | 414 | 0 |

- WorkloadInstance: 4.5x and 3.9x faster on median server time, 3.7x fewer DB
  hits. The before and after ranges do not overlap in either case (slowest
  after 553 ms, fastest before 1540 ms).
- CloudResource, every path whitelisted: the label test removes nothing and
  adds one check per path. DB hits rose by 81 (7.8%) and 65 (6.9%) for 76 and
  61 paths. The time is under the 1 ms timer resolution in both variants, so
  no time difference is measured; the cost is about one DB hit per path. This
  is the class that gets slightly slower in work done, and on this corpus the
  absolute size is tiny.
- Workload: 3.0x fewer hits on 137 paths, none whitelisted; time is under
  resolution.
- Path-set equivalence held for all five anchors, both directions, four reads.
  The first pass on wi_2 showed 215 paths differing in both directions
  including between two reads of the same variant, with the same path count:
  graph churn (relationship ids rewritten by ingestion), not the change. Two
  repeat passes were identical and equal across variants. The 11-row (or 0-row)
  output was identical and in the same order in every read.
- Host load average on the driving machine was 11 to 21 at the start of the
  timing run and 15 at the end.

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

- The anchor classes were timed unevenly. Repository anchors have nine
  cases above. Workload, WorkloadInstance and CloudResource have the few cases in
  the next section. TerraformModule and DataAsset anchors were not timed: no
  such node on this corpus has an outgoing relationship, so there is nothing to
  traverse. CloudResource timings sit below the 1 ms resolution of the timer, so
  that class rests on PROFILE DB hits.
- No deployed latency. The request-time effect needs a rebuilt image replayed
  against the same corpus; only statement server time is measured here.
- The expansion cost is unchanged: every path of up to four hops is still
  enumerated. For `repo-B` the remaining 145 ms is that expansion.
- A cheap emptiness pre-check (a pruned BFS before the enumeration) was
  measured at 57 to 66 ms in the diagnosis and is not part of this change.
- The scoped-grant outgoing statement and environment-filtered or non-default
  depth reads were not measured; they are unchanged or not on the measured path.
- One shared graph, one Neo4j version. Other data shapes, or a Neo4j planner
  that reorders the conjuncts, are not covered.
