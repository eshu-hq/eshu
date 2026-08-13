# Module node identity is (name, language)

Found while working #4594.

## The problem

The canonical import-graph `Module` node was MERGEd on its name alone:

```cypher
UNWIND $rows AS row
MERGE (m:Module {name: row.name})
SET m.lang = coalesce(m.lang, row.language),
    m.evidence_source = 'projector/canonical'
```

One global node per module name, across every language in the corpus. The
IMPORTS edge resolved its target the same way, `MATCH (m:Module {name:
row.module_name})`. So a Go file importing `time` and a Python file importing
`time` pointed at the same node, and that node carried one language.

Two corpus fixtures collide today: `os` and `time` are each imported from both
Go and Python files (measured, below). `path` (Go and JavaScript) and `basic`
(Ruby and Python) are the same shape.

Two things were wrong, not one:

1. **Wrong graph truth.** The graph asserted that a Go file imports a Python
   module. A query filtering `(m:Module {name: 'time', lang: 'go'})` silently
   missed every Go importer whose node had been stamped `python`.
2. **The stamped language was not even stable.** `coalesce(m.lang,
   row.language)` reads as first-writer-wins, but measured against the pinned
   NornicDB build an `UNWIND` batch carrying both languages evaluated every
   row's `SET` against the pre-batch property snapshot, so the LAST row won.
   The projected language tracked batch order.

The same collision existed one layer up, in the projector. `mergeImportModules`
and `extractImportsFromFiles` both keyed their dedupe map on the module name, so
the second language's module was dropped in process and never reached the
writer. Fixing only the Cypher would have left that in place.

## The identity, and why not repository or scope

`(name, language)`.

A Go `time` and a Python `time` are unrelated modules that share a name, so they
are two nodes. Two repositories importing the same Go `time` still share one
node, and that sharing is the point: the shared node IS the cross-repository
dependency link the import graph exists to provide.

Adding repository or scope to the key would break that link. The read surface
already encodes the same intent independently: `relationshipVerbCatalog`
(go/internal/query/relationships_catalog_cypher.go) marks IMPORTS
`targetAttributable: false`, documented as "a shared/global entity with no
tenant attribution", and `relationshipEdgesScopeWhereClause` binds the #5167
scoped-grant predicate to the source endpoint only for this verb. A repo-scoped
Module would contradict a deliberate design decision, not merely change a key.

### Consumers checked

| Consumer | Effect |
| --- | --- |
| `directImportRowsCypher`, `packageImportRowsCypher`, `sourceModuleImportRowsCypher`, `fileImportCycleEdgeRowsCypher` (go/internal/query/code_import_dependencies_queries.go) | Every one reaches Module through an anchored `-[rel:IMPORTS]->` edge, so rows equal edges and the split changes no cardinality. `target_module.lang` in `importRowPredicates` becomes correct instead of batch-order dependent. |
| `sourceModuleFilesCypher`, `targetModuleFilesCypher` | Match `(:Module {name})<-[:CONTAINS]-(:File)`, which is the semantic declaration Module (uid-keyed). Untouched. |
| `relationshipEdgesCypher` | Projects `coalesce(t.id, t.uid, t.name, t.path)` as `target_id`. Import Modules have only `name`, so `target_id` stays the module name and no response field changes. Two same-named modules now project the same `target_id`, which is no worse than one node carrying a wrong language. |
| `nornicDBRelationshipEntityLabels` / `get_entity_context` | Resolve by uid, so they address the semantic Module. Untouched. |
| Orphan sweep (`OrphanSweepLabelModule`) | **Does** rely on Module.name being unique inside its class. See below. |

### Empty language

An empty language is a value, not a wildcard. A module discovered only from
files whose language could not be determined gets its own `lang: ''` node. It
never merges into a languaged one, because attributing an unattributable import
to a language the evidence does not support is exactly the failure being fixed.
It cannot multiply either: every unknown-language importer of one name shares
that single node, so the rule costs at most one extra node per module name.
`TestExtractImportsGivesUnknownLanguageItsOwnModule` pins it. The staged corpus
produces no such node today (both collisions are go/python).

## Upgrade path on an already-indexed deployment

**No operator step is required.** Reasoning, from the code:

The pre-fix writer always SET `lang`, even to the empty string, so every
existing canonical Module node already carries a `lang` property. The new
`MERGE (m:Module {name, lang})` therefore MATCHES the existing node for whichever
language got stamped, and re-adopts it. Only the other languages mint new nodes.
`TestLiveCanonicalModuleIdentityReadoptsExistingLanguagedNode` proves this
against the pinned backend: seeded with one legacy `{name: time, lang: go}`
node, a projection carrying Go and Python `time` ends at two nodes, not three.

A node is left behind only when its stamped language matches no importer any
more. That is bounded: at most one per module name, and only for names whose
projected language changes.

**What reaps a left-behind node is the #5327 orphan sweep, not
generation-retract.** Canonical import Module nodes carry no `repo_id` and no
`generation_id`, so `canonicalNodeRetractEntityTemplate` — `WHERE n.repo_id =
$repo_id AND ... n.generation_id <> $generation_id` — cannot match one; there is
no generation-retract path for this label at all. The orphan sweep owns them
explicitly: `OrphanSweepLabelModule` with the class predicate `n.uid IS NULL`
(go/internal/storage/cypher/orphan_sweep_guards.go), which selects exactly the
canonical import modules and excludes the uid-keyed semantic ones. A
disconnected node is marked, then deleted after `defaultOrphanSweepTTL`
(7 days).

### The one consumer that degrades

The orphan sweep anchors on `n.name`, and after this change that key is no
longer unique inside the class it owns. Its Go-side anti-join keys connectivity
by that single string, so two same-named modules share one entry and the pair
counts as connected when EITHER node has a relationship.

The degradation is one-directional and bounded:

- It never deletes a connected node, and never deletes a disconnected node whose
  same-named sibling is connected.
- A disconnected module whose same-named sibling is still imported is not swept.
  It lingers as a disconnected node. No query reaches it — every Module read
  arrives through an IMPORTS or CONTAINS edge it no longer has — and it stays
  counted in `GraphOrphanNodeCounts`, so it surfaces as a Module orphan count
  that does not drain rather than as wrong query truth.

`TestLiveOrphanSweepModuleSameNameDifferentLanguages` pins both directions
against the pinned backend so this cannot drift silently. Making the sweep exact
means threading a composite key through its S1/S2 reads and its three
key-anchored writes, which is a larger change than the identity fix and is
deliberately not folded in here.

## Schema

No schema change. `module_name_lookup` (a plain index on `(m:Module) ON
(m.name)`) still anchors the MERGE, and it must stay an index rather than a
constraint: the semantic entity path MERGEs Module on `uid` and shares the
label, so a uniqueness constraint on `name` — or on `(name, lang)` — fails on
those nodes.

That the index is enough is measured, not assumed. NornicDB's `findMergeNode`
(pkg/cypher/merge.go at the pinned revision) tries an exact composite index,
then a unique constraint, then the smallest single-property index candidate set,
and marks `MergeScanFallbackUsed` only when none of those matched.

## Performance Evidence

Backend: NornicDB source revision `3722b483c02c38a8e046d198f8768f200f31023c`
(the pin the Compose default `eshu-nornicdb-pr290:3722b483c02c` is built from),
in-process `StorageExecutor` over a namespaced memory engine. Schema state:
production `module_name_lookup` index plus the `File.path` uniqueness
constraint. Input: the same two-row `UNWIND` batch (`time`/go, `time`/python)
against a Module label populated with N same-name candidates.

Hot-path trace, exact Eshu query constants read from source by `go/parser`
(harness `testdata/nornicdb/eshu_exact_module_identity_trace_test.go`, retained
output `docs/internal/evidence/module-identity-hotpath-trace.txt`):

| Eshu query constant | UnwindMergeChainBatch | MergeSchemaLookupUsed | MergeScanFallbackUsed | OuterScanFallbackUsed |
| --- | --- | --- | --- | --- |
| `canonicalNodeModuleUpsertCypher` | true | true | false | false |
| `canonicalNodeImportEdgeCypher` | true | true | false | false |

Neither the two-property MERGE nor the two-property MATCH falls back to a Module
label scan.

Before/after, `GOMAXPROCS=1`, `-benchtime=300x -count=5`, median ns/op, same
host, same store shape. "legacy" is the verbatim pre-change statement, "new" is
the production constant read from Eshu source:

| Same-name candidates | legacy `{name}` | new `{name, lang}` | ratio |
| ---: | ---: | ---: | ---: |
| 1 | 42,830 | 22,552 | 0.53x |
| 25 | 44,364 | 24,291 | 0.55x |
| 200 | 46,164 | 27,642 | 0.60x |

Performance Evidence: on NornicDB source revision
`3722b483c02c38a8e046d198f8768f200f31023c` with the production
`module_name_lookup` index applied, the same two-row `UNWIND` batch, and
`GOMAXPROCS=1 -benchtime=300x -count=5`, the canonical Module upsert's median
cost fell from 42,830 ns/op to 22,552 ns/op at 1 same-name candidate, 44,364 to
24,291 at 25, and 46,164 to 27,642 at 200. Both the (name, lang) MERGE and the
(name, lang) IMPORTS-edge MATCH report `MergeSchemaLookupUsed=true`,
`MergeScanFallbackUsed=false`, `OuterScanFallbackUsed=false` on the exact Eshu
query constants, so neither falls back to a Module label scan. Statement count,
batch size, and write phase are unchanged; the projected node count for the
staged corpus moves from 42 to 44.

The new statement is faster, and the reason is not the key: both shapes resolve
through the same `module_name_lookup` index and load the same candidate set. The
saving is the dropped `SET m.lang = coalesce(m.lang, row.language)` — a function
evaluation and a property write per row that the new identity makes unnecessary,
because the language is settled by the MERGE. Cost grows gently with candidate
count on both shapes (legacy +8%, new +22% from a much lower base) across a
200x candidate range, which is the shape of an index lookup plus a linear
candidate compare, not a label scan.

## Correctness proof

Against the pinned backend through Eshu's own Bolt driver, driving the
production statement builders (`buildModuleStatements`,
`buildStructuralEdgeStatements`) rather than hand-written Cypher:

```text
ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:17692 ESHU_CYPHER_BOLT_DATABASE=nornic \
  go test ./internal/storage/cypher -run TestLiveCanonicalModuleIdentity -count=1 -v
PASS
```

The guard is failable in both halves, proven by mutation:

- Reverting the MERGE key to `{name}`: `Module "time" nodes: got langs [""],
  want exactly [go python]`.
- Reverting only the IMPORTS edge MATCH to `{name}`, MERGE key left correct:
  `file /eshu-test/modident/py/main.py imports Module{name="time",
  lang="go"}, want {name="time", lang="python"}`.

On unmodified `origin/main` the same assertions read `got langs ["python"]` —
one node, and stamped with the language of the last row in the batch, which is
the batch-order behavior described above.

Projector side, four guards covering both dedupe seams and both producers, each
proven failable by reverting its map key to the name alone:

```text
got 1 module rows [{Name:time Language:}], want 2          (extractImportsFromFiles)
got 1 module rows [{Name:basic Language:ruby}], want 2      (mergeImportModules)
got 1 module rows [{Name:inheritance Language:ruby}], want 2 (extractModulesFromEntities)
```

## Golden corpus

`graph.node_counts.Module` floor raised 20 -> 44. 44 is measured, not chosen:
running the production parser (`parser.DefaultRegistry` + `Engine.ParsePath`)
and the production extractor (`extractImportsFromFiles`) over the exact
`scripts/lib/golden-corpus-fixtures.sh` corpus gives 42 distinct module names
and 44 distinct (name, language) pairs, with 2 names imported from two
languages: `os` and `time`, both Go and Python. It is a true floor because
semantic declaration Module nodes only add to the label total.

That floor catches a projection collapse; it does not isolate the identity. With
only two colliding names the identity delta is +2, too small for a count range
to resolve. The identity is asserted directly by the live test and the trace
harness above. The B-12 note records this explicitly so a later reader does not
mistake the floor for an identity assertion.

Remaining verification: the full `scripts/verify-golden-corpus-gate.sh` live run
has not been executed on this branch. Host load average was above the gate's
practical ceiling for the whole session, and a contended run produces timing
findings that are not attributable to the diff.

## Observability Evidence

No-Observability-Change: this change adds no metric, span, or log, and removes
none. The Module write keeps the same phase, batch shape, and
`CanonicalAtomicWrites` / `CanonicalWriteDuration` instrumentation it already
had; the statement count and batching are unchanged. The one operator-visible
signal that shifts is the existing `GraphOrphanNodeCounts` Module gauge, which
is the surface the orphan-sweep degradation above reports through, and it needed
no code change to do so.
