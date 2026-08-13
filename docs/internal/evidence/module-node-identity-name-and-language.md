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

### The sweep key, now exact

The sweep used to anchor on `n.name`, which stopped being unique inside the
class it owns the moment Module identity became `(name, lang)`. Two same-named
modules shared one entry in its Go-side anti-join, so a disconnected one counted
as connected whenever its sibling was still imported: never marked, never
deleted, and permanently counted in `GraphOrphanNodeCounts`. The residue grew
one node per module name whose projected language ever changed.

The key is `(name, lang)` now, threaded through the S1 candidate read, the S2
connectivity read, and all three key-anchored writes. Single-property labels
emit byte-identical Cypher and the same `$keys`/`$cursor` parameter shapes.

Two properties of the pinned backend shaped it, both verified through the Bolt
driver before the code was written:

- `ORDER BY` over the RETURN aliases is not honoured — five rows across three
  names came back unordered — while ordering the `WITH`-projected values is. The
  composite read projects through a `WITH` and orders on that.
- A Module written before the identity cutover can carry no `lang` property at
  all, because the old writer settled it with
  `SET m.lang = coalesce(m.lang, row.language)` and that removes the property
  when the row had no language. A bare `n.lang > $cursor_1` never matches such a
  node, so it would sit outside every page forever. Every property after the
  anchor compares through `coalesce(n.lang, '')`.

Paging is the risky part of a composite key, so it is tested rather than
inferred. `TestOrphanSweepCompositeCursorVisitsEveryRowExactlyOnce` runs five
rows across three names at two rows a page, so a boundary lands inside a name's
language group in both positions, and asserts only the two connected rows
survive. `TestOrphanSweepCompositeCursorResumesMidName` pins the resume itself.
The same walk was run live first — six rows, two a page, each key visited
exactly once. `TestLiveOrphanSweepModuleSameNameDifferentLanguages` now asserts
the disconnected sibling is deleted and the connected one and its live IMPORTS
edge are untouched, and
`TestLiveOrphanSweepModuleWithNoLanguageIsStillReachable` covers the
language-less node.

### Stale writers during a rolling upgrade

The identity cutover changes no DDL, and the schema fingerprint was a digest of
the backend name plus the ordered DDL and nothing else. An old pod therefore
computed the same value, `RequireCompatible` admitted it, and its name-only
`MATCH (m:Module {name: row.module_name})` bound both language nodes —
attaching a Go file's IMPORTS edge to the Python module. The bad edge outlives
the rollout, because `canonicalNodeRefreshCurrentFileImportEdgesCypher` only
deletes IMPORTS edges for the paths a generation projects.

The digest now covers a write-identity contract (`graphWriteIdentityContract`)
as well as the DDL, and the resulting fingerprint lists no compatible
predecessors. Bootstrap applies byte-identical statements; `StatementCount` is
unchanged.

A composite `(m.name, m.lang)` index was the other candidate lever, and it is
not available on this backend. Cypher DDL routes every index to
`storage.SchemaManager.AddPropertyIndex`, which keys by label plus **first**
property, so `module_name_lookup` already owning `Module:name` wins and the
two-property statement returns success while registering nothing. Reproduced
through the Bolt driver against `eshu-nornicdb-pr290:3722b483c02c`:

```text
[A-collide]       CREATE INDEX kx_collide ... ON (m.name, m.lang)   rows=0
[A-after-collide] SHOW INDEXES
                    kx_name_lang ONLINE PROPERTY NODE [KeyProbe] [name]
                    (kx_collide absent)
[A-free-key]      CREATE INDEX kx_free ... ON (m.lang, m.name)      rows=0
[A-after-free]    SHOW INDEXES
                    kx_free ONLINE PROPERTY NODE [KeyProbe] [lang name]
```

An A/B of the module upsert across that "with and without" pair measured
138,297–144,054 ns/op against 146,166–156,465, but both arms had the identical
index set, so that difference is noise and is not evidence for anything.

### target_id on IMPORTS edges

Canonical import Modules carry no `id` and no `uid`, so the relationships
catalog resolved `target_id` to the bare module name and the two language nodes
projected the same value. It is language-qualified now. Measured against the
pinned backend with four seeded edges:

| edge target | before | after |
| --- | --- | --- |
| `Module{name: "time", lang: "go"}` | `time` | `time@go` |
| `Module{name: "time", lang: "python"}` | `time` | `time@python` |
| `Module{name: "time"}` | `time` | `time@` |
| a Module carrying a `uid` | its `uid` | its `uid` |

The query-plan gate's pinned `cypher_sha256` does not move: QP-RELATIONSHIPS-EDGES
binds the CALLS representative of this 20-verb family, and CALLS keeps the
default projection, so a verb-specific override is invisible to it. The
`ORDER BY` tie-breaker is left alone for a measured reason — `ORDER BY` over a
`CASE` expression is not honoured on this backend, so putting the expression
there would move a pinned hash for a sort that does not happen.

## Schema

No DDL change. `module_name_lookup` (a plain index on `(m:Module) ON (m.name)`)
still anchors the MERGE, and it must stay an index rather than a constraint: the
semantic entity path MERGEs Module on `uid` and shares the label, so a
uniqueness constraint on `name` — or on `(name, lang)` — fails on those nodes.

That the index is enough is measured, not assumed. NornicDB's `findMergeNode`
(pkg/cypher/merge.go at the pinned revision) tries an exact composite index,
then a unique constraint, then the smallest single-property index candidate set,
and marks `MergeScanFallbackUsed` only when none of those matched.

The schema **fingerprint** does move, even though the DDL does not, because the
digest now covers the write-identity contract. See "Stale writers during a
rolling upgrade" above for why that is the fence and why the composite index is
not.

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

## Review follow-ups on the follow-up PR (#6106)

Three findings on the branch above, and what each one changed.

### The fingerprint fences admission, not a writer already running

The fingerprint change decides whether a writer may **start**.
`RequireCompatible` is called once, in `cmd/reducer/run.go`,
`cmd/projector/main.go`, and `cmd/ingester/main.go`, before the service loop,
and nothing looks at the marker again. So emptying the compatible list refuses
the next pod to start and does nothing about the pods already serving — which,
with `schemaBootstrap.useHelmHooks=true`, is every pod of the outgoing release,
because `job-schema-bootstrap.yaml` carries `helm.sh/hook: pre-install,pre-upgrade`.
The reviewer is right, and the PR's earlier answer treated an admission gate as
if it were a write fence.

Half of that is now closed and half of it cannot be, so both are stated plainly:

- **Closed.** `graphschemacompat.WriteFence` re-reads the marker on the write
  path, and `CanonicalNodeWriter.WithSchemaWriteFence` makes every canonical
  write ask it first. A writer the applied marker stops admitting fails before
  building a statement instead of writing under an identity the schema no longer
  describes. The refusal is retryable, so the work waits in the queue for the
  pod that replaces this one rather than dead-lettering a backlog. Decisions are
  cached for 30 seconds, so it costs one indexed marker read per writer per
  interval, not one per write. A marker that cannot be read holds the previous
  decision — failing closed there would turn one Postgres blip into a
  simultaneous graph-write outage across every writer, which is worse than the
  gap being closed.
- **Not closed, and not closable from here.** A writer built before this fence
  contains no call to it. Nothing added to this repository changes what that
  binary does, and no schema object can stop it either: its harmful operation is
  `MATCH (m:Module {name: row.module_name})`, an ordinary read, and Module
  cannot take a uniqueness constraint at all because the semantic entity path
  MERGEs Module on `uid` and shares the label. For the #6102 cutover
  specifically, the only thing that stops the outgoing pods is stopping them —
  scale ingester, projector, and resolution engine to zero before bootstrap
  records the marker. That is now written down in
  `docs/public/deployment/service-runtimes-bootstrap.md` rather than left to be
  discovered.

Proven by mutation, on the built code:

```text
# fence call removed from CanonicalNodeWriter.Write
--- FAIL: TestCanonicalNodeWriterStopsAtARefusingSchemaFence
    Write() error = nil, want the schema refusal
--- FAIL: TestCanonicalNodeWriterWritesWhenTheSchemaFenceAdmits

# fence treats every error as a refusal
--- FAIL: TestWriteFenceHoldsItsDecisionWhenTheMarkerCannotBeRead
    Check() with the marker unreadable = query graph schema compatibility
    marker: dial tcp: connection refused, want nil; a database blip is not a
    refusal

# fence stops caching its decision
--- FAIL: TestWriteFenceReusesItsDecisionWithinTheInterval
    Check() 1 error = graph schema incompatible for backend nornicdb...
```

### An absent Module language is not an empty one

The sweep read every identity property after the anchor through
`coalesce(n.lang, '')`, which gave a Module with no `lang` property and a Module
with `lang: ''` the same sweep key. `MERGE (m:Module {name, lang})` does not
match a node without the property, so those are two nodes, and the writer treats
them as two. The connected empty-language node then answered the S2
connectivity read for the pair and the disconnected lang-less one was read back
as connected: never marked, never deleted, and still counted in
`GraphOrphanNodeCounts`. The failure only ever runs in the safe direction —
under-deletion, never a wrong delete — but it is the same masking the composite
key exists to remove.

The default is now `'<absent>'`, a value the property it stands in for cannot
hold, and the same expression renders it in the S1 ORDER BY, the S1 keyset
predicate, the S2 read, and all three key-anchored writes, so the paging order
and the anti-join agree on where an absent property sits.

The PR's stated reason for the coalesce was wrong and is corrected in the code.
It claimed the pre-cutover writer's `SET m.lang = coalesce(m.lang, row.language)`
removed the property when a row carried no language. It cannot:
`buildModuleStatements` builds every row from `projector.ModuleRow.Language`, a
Go `string`, which reaches Cypher as `''` and never as null, in the current
statement and in both earlier ones in this repository's history. A canonical
Module with no `lang` is not something an Eshu writer produces; it comes from
outside the writer, which is why the sweep must still reach it and must not fold
it into a language it was never given.

`TestOrphanSweepSweepsLangLessModuleBesideConnectedEmptyLanguageOne` covers the
combined upgrade shape the reviewer asked for — one lang-less disconnected node
beside a same-named connected `lang: ''` node — rather than the two shapes
separately. The fixture substitutes whatever coalesce default the statement
under test emits, so reverting the default collapses the pair there too:

```text
# coalesce default reverted to ''
--- FAIL: TestOrphanSweepSweepsLangLessModuleBesideConnectedEmptyLanguageOne
    surviving Module nodes = [["time" ""] ["time" "\x1fabsent"]], want [["time" ""]]
```

### Two test-quality findings

`TestBuildConnectedKeysQueryUsesConcreteRelationshipVariable` compared
`fmt.Sprintf("%v", stmt.Parameters["keys"])` against a fixed string. Go
randomizes map iteration order, so the rendering of a composite label's
`map[key_0:... key_1:...]` rows is not a stable value and the assertion could
fail on order alone. It compares the parameter structurally now. Still failable:
renaming the emitted `key_%d` columns to `k_%d` gives

```text
--- FAIL: TestBuildConnectedKeysQueryUsesConcreteRelationshipVariable/Module
    keys parameter = [...{"k_0":"a", "k_1":""}...], want [...{"key_0":"a", "key_1":""}...]
```

and 20 consecutive runs of the affected tests pass, which a formatted-string
comparison could not be relied on to do.

`advanceCursor`'s comment said the cursor wraps to `""` at the end of a label.
The cursor is `nil` there, the same start-of-label value `candidateCursor`
returns before the first read; `cursorValue` is what turns it into the
empty-string comparison operand. The comment says that now.

### Cost of the three follow-up fixes

No-Regression Evidence: none of the three changes adds a round trip or a scan.
The orphan sweep issues exactly the statements it did before — one S1 candidate
read, chunked S2 connectivity reads, a bounded S2 re-verify only on a sweeping
cycle, and up to three key-anchored writes — and the coalesce default change is
one literal inside an expression that was already there, on an already-loaded
candidate row, with the Module statements still anchored on `name` inline in the
MATCH pattern so they resolve through `module_name_lookup` rather than scanning
the label. The schema write fence adds one indexed single-row Postgres read per
writer per 30-second interval (`graph_schema_applications` by backend, ORDER BY
applied_at DESC LIMIT 1), not one per write:
`TestWriteFenceReusesItsDecisionWithinTheInterval` asserts the read count, 1
across five checks inside an interval and 2 across two. Backend:
`eshu-nornicdb-pr290:3722b483c02c`. No new Cypher statement is emitted by any of
the three.

Observability Evidence: no metric, span, or log is added or removed, and the new
refusal path is not silent. `CanonicalNodeWriter.Write` returns the fence error
as a retryable projector error, so it lands on the projector queue row with the
full "graph schema incompatible for backend X: runtime expects fingerprint A,
latest applied fingerprint is B" message and reaches an operator through the
existing queue and status surfaces, alongside the identical startup message in
`runtime.startup.failed`. `docs/public/deployment/service-runtimes-bootstrap.md`
and `go/internal/graphschemacompat/AGENTS.md` both name that message on a
retrying row as the signature of a mid-flight refusal, so the 3 AM read is "a
schema application landed under this pod", not "the graph writer is stuck".

## Follow-up changes (#6102 review)

The three review follow-ups touch the orphan sweep, the schema fingerprint, and
the relationships `target_id` projection. None of them changes how much work any
path does.

No-Regression Evidence: the orphan sweep issues the same round trips per label
per cycle it did before — one S1 candidate read, chunked S2 connectivity reads,
a bounded S2 re-verify only on a sweeping cycle, and up to three key-anchored
writes. The composite key changes the statement text, not the count, and the
Module statements still anchor on `name` inline in the MATCH pattern, so they
resolve through `module_name_lookup` rather than scanning the label; the second
property is a comparison on the already-loaded candidate. Backend:
`eshu-nornicdb-pr290:3722b483c02c` over Bolt.
`TestLiveOrphanSweepModuleSameNameDifferentLanguages` (two cycles, three seeded
nodes, one IMPORTS edge) runs in 0.26s and
`TestLiveOrphanSweepModuleWithNoLanguageIsStillReachable` in 0.12s against it.
No throughput benchmark was taken, and the reason is that there is nothing to
compare: the round-trip shape is identical, and the corpus-scale Module
population is the same one the pre-change sweep already paged. The schema
fingerprint change adds no DDL, so `SchemaStatementsForBackend` returns a
byte-identical list and `StatementCount` is unchanged — asserted by
`TestWriteIdentityContractMovesTheFingerprintWithoutMovingTheDDL`. The
`target_id` change adds one scalar expression over an already-bound target node
in an already-bounded, `LIMIT`-capped projection; it adds no MATCH, no scan, and
no ORDER BY term.

The one measurement that could have justified new work is recorded above and
came back negative: a composite `(m.name, m.lang)` index cannot be created on
this backend, and the A/B that appeared to favour it had the identical index set
in both arms.

No-Observability-Change: no metric, span, or log is added or removed. The
`GraphOrphanNodeCounts` Module gauge keeps its name and shape; what changes is
that a Module orphan now drains instead of staying counted, which is why the
operator note in the telemetry reference no longer describes an expected
residue.
