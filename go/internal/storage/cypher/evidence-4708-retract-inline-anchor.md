# SQL relationship retract: inline-property anchor + UNWIND (#4708)

Classification: **Wall-clock win** (warm re-ingest graph-write path). Output-preserving
(deletes the identical edge set — proven 0/0). Not a correctness change to graph truth.

## Root cause

The SQL relationship whole-scope retract deleted stale edges with
`MATCH (source:Function)-[rel:QUERIES_TABLE]->() WHERE source.repo_id IN $repo_ids AND rel.evidence_source = $evidence_source DELETE rel`
(and five sibling label/type pairs; by-file variants use `source.path IN $file_paths`).
On NornicDB a **compound** `WHERE <node.prop> AND <rel.prop>` is not split, so the
executor cannot extract the start-node predicate for its property-index seek
(`tryCollectNodesFromPropertyIndex`) and degrades to a full label scan of the
source label (~572k `:Function` nodes on a 910-repo graph). This is the warm
changed-repo re-ingest half of #3624 (the cold first-ingest half is the merged
first-projection skip, #4710).

## Fix

Rewrite each retract to `UNWIND` the scope list and anchor the source node with
an **inline property** rather than a `WHERE ... IN` predicate:

```
UNWIND $repo_ids AS repo_id
MATCH (source:Function {repo_id: repo_id})-[rel:QUERIES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel
```

By-file variants `UNWIND $file_paths AS file_path` with `{path: file_path}`. The
inline property binds the source via its (index-served) property and drives the
relationship expansion from the tiny bound set; only the relationship predicate
remains in `WHERE`. Same parameters (`$repo_ids` / `$file_paths` /
`$evidence_source`), same deleted edge set.

## Performance Evidence:

Measured on a resident full-corpus NornicDB (`eshu-nornicdb-main:d97f02c1`,
1.1M nodes / 1.74M rels, `:Function`=572k) via the neo4j HTTP tx endpoint, real
repo_id (`repository:r_eae4cd60`):

| shape | time |
|---|---|
| OLD `MATCH (source:Function)-[rel]->() WHERE source.repo_id IN $repo_ids AND rel.evidence_source=$es` | 11.4s |
| NEW `UNWIND $repo_ids AS repo_id MATCH (source:Function {repo_id: repo_id})-[rel]->() WHERE rel.evidence_source=$es` | **0.002s** |
| NEW by-file `UNWIND $file_paths AS file_path MATCH (source:Function {path: file_path})-[rel]->() WHERE rel.evidence_source=$es` | 0.001s |

~5700x for the `:Function` source label (the QUERIES_TABLE retract): it is the
large label (572k nodes) and the only one of the six SQL source labels with a
`repo_id` and `path` node index (`go/internal/graph/schema_tables.go`), so the
inline anchor is index-served (0.002s) where the old compound-WHERE full-scanned
(11.4s).

The other five statements source from the SQL entity labels, which are small in
this corpus — `SqlTable`=916, `SqlView`=32, `SqlFunction`=5, `SqlTrigger`=0
nodes. They have no `repo_id`/`path` index, so the new inline anchor still label-
scans, but over a tiny population it is already fast and is no worse than the old
compound-WHERE (which also could not seek): measured NEW
`UNWIND [rid] MATCH (source:SqlTable {repo_id: rid})-[rel:HAS_COLUMN]->() WHERE rel.evidence_source=$es`
= 0.015s. So the change is a large wall-clock win on the dominant `:Function`
statement and a no-regression correctness-equivalent rewrite on the small-label
statements. If the SQL entity labels ever grow large, adding their `repo_id` +
`path` node indexes (mirroring the `Function`/`ShellCommand` precedent) would
extend the index-served win to all six — tracked as a follow-up on #4708.

Exact-equivalence (output-preserving, 0/0): on real HAS_COLUMN data
(`repository:r_4db0b361`, evidence_source `finalization/workloads`),
OLD `WHERE source.repo_id IN [rid] AND rel.evidence_source=$es` returns 1 edge and
NEW `UNWIND [rid] MATCH (source:SqlTable {repo_id: rid})-[rel] WHERE rel.evidence_source=$es`
returns 1 edge (identical); empty `$repo_ids` deletes 0 both ways. Unit coverage:
`go test ./internal/storage/cypher/ -run SQL -count=1` (16 pass) asserts the new
UNWIND + inline-anchor shape and that the slow `source.repo_id IN` /
`source.path IN` predicates are gone.

## Observability Evidence:

No-Observability-Change. The retract is issued through the same grouped/autocommit
canonical-retract path with the same `OperationCanonicalRetract` operation and the
same parameters; only the Cypher text changed. No new metric, span, log, or status
field, and no operator-facing signal is altered.

## Scope

This PR covers the `sql_relationships` domain (all label-scoped sources). The
same rewrite applies to the other repo-wide-retract domains as follow-ups:
`shell_exec`, `rationale_edges`, `handles_route`/`runs_in`/`invokes_cloud_action`
(labeled sources — trivial), and `inheritance_edges` (labelless `(child)` anchor
needs label-enumeration over the class-like source labels, since a labelless
inline anchor cannot select an index — measured 5.3s).

The non-GroupExecutor fallback retract const (`retractSQLRelationshipEdgesCypher`
in `canonical.go`, a single labelless `MATCH (source)` with rel-type alternation
and `WHERE source.repo_id IN`) is intentionally left unchanged: it is reached
only when the executor is not a GroupExecutor, which is not the production
reducer path (`cmd/reducer/main.go` wires a GroupExecutor). Rewriting that seam
is a separate follow-up on #4708.
