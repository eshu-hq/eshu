# Module node identity: review follow-ups on PR #6106

Split out of [Module node identity is (name,
language)](module-node-identity-name-and-language.md), which carries the
identity change itself, its performance and correctness proof, and the golden
corpus floor. This file holds the three findings raised on the follow-up PR and
what each one changed.

## The fingerprint fences admission, not a writer already running

The fingerprint change decides whether a writer may **start**.
`RequireCompatible` is called once, in `cmd/reducer/run.go`,
`cmd/projector/main.go`, and `cmd/ingester/main.go`, before the service loop,
and nothing looks at the marker again. So emptying the compatible list refuses
the next pod to start and does nothing about the pods already serving — which,
with `schemaBootstrap.useHelmHooks=true`, is every pod of the outgoing release,
because `job-schema-bootstrap.yaml` carries `helm.sh/hook: pre-install,pre-upgrade`.
The reviewer is right, and the PR's earlier answer treated an admission gate as
if it were a write fence.

Part of that is now closed and part of it is not, so each part is stated
plainly rather than rounded up to "fenced":

- **Closed, for one writer.** `graphschemacompat.WriteFence` re-reads the marker
  on the write path, and `CanonicalNodeWriter.WithSchemaWriteFence` makes every
  write through that writer ask it first. A writer the applied marker stops
  admitting fails before building a statement instead of writing under an
  identity the schema no longer describes. The refusal is retryable, so the work
  waits in the queue for the pod that replaces this one rather than
  dead-lettering a backlog. Decisions are cached for 30 seconds, so it costs one
  indexed marker read per fenced writer per interval, not one per write. A marker
  that cannot be read holds the previous decision — failing closed there would
  turn one Postgres blip into a simultaneous graph-write outage across every
  fenced writer, which is worse than the gap being closed.
- **Not closed for a pre-fence release.** A writer built before this fence
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
- **Not closed for the reducer, on the current release.** A #6106 review caught
  the first version of this section calling the gap historical when it is not,
  and a #6121 review caught the replacement describing the unfenced reducer
  writers as "the specialized cloud, Kubernetes, and IAM writers", which is not
  the whole set either. The full inventory is below.

### What deployment ordering actually guarantees

Not what an earlier draft of this record and of
`go/internal/graphschemacompat/doc.go` said. Both claimed the unfenced reducer
writers "are stopped by deployment ordering". They are not, and the claim is the
same shape as the overclaim this whole record exists to correct.

Read from the chart and the Compose file:

- `deploy/helm/eshu/templates/job-schema-bootstrap.yaml:15-17` annotates the
  schema Job `helm.sh/hook: pre-install,pre-upgrade` at weight `-10`, so Helm
  runs it to completion before applying the release's ordinary manifests.
- `deploy/helm/eshu/templates/deployment-resolution-engine.yaml` is an ordinary
  `Deployment` with no hook annotation and `replicas:
  {{ .Values.resolutionEngine.replicas }}`. At the moment the Job records the
  new marker, the outgoing generation of those pods is still running at that
  replica count, and it keeps writing until the rolling update replaces it.
- `docker-compose.yaml` gates `bootstrap-index`, `ingester`, `projector`, and
  `resolution-engine` on `db-migrate: service_completed_successfully`. That is a
  start condition on a container that is not yet running.

So ordering guarantees one thing: a graph writer does not **start** before the
marker for its schema is recorded. It does not stop a writer that is already
running, in either deployment path. Stopping those is a manual step.

### The reducer graph writer inventory

Every graph writer `cmd/reducer` constructs, enumerated from the constructor
call sites rather than from memory of what the reducer "does":

```bash
rg -n 'sourcecypher\.New|graphowner\.New' go/cmd/reducer --glob '!*_test.go'
```

| Construction site | Writer |
| --- | --- |
| `main.go:360` | `EdgeWriter` (shared-projection edges, every domain) |
| `endpoint_presence_wiring.go:90` | a second `EdgeWriter` |
| `neo4j_wiring.go:252,260` | `SemanticEntityWriter` / `SemanticEntityWriterWithCanonicalNodeRows` |
| `secrets_iam_graph_wiring.go:63` | `SecretsIAMGraphWriter` |
| `graph_orphan_sweep_wiring.go:27` | `OrphanSweepStore` |
| `canonical_graph_writers.go:78-127` | every field of the `canonicalGraphWriters` struct |

That last row is the one the old "cloud, Kubernetes, and IAM writers" phrasing
under-described. The struct's fields also include `incidentRoutingEvidence`,
`codeTaintEvidence`, `codeInterprocEvidence`, `provenanceEdge`,
`crossplaneSatisfiedByEdge`, `observabilityCoverageEdge`, and
`s3ExternalPrincipalGrant`, none of which read as cloud/Kubernetes/IAM. The
struct definition at `canonical_graph_writers.go:17-68` is the list; the
`graphowner` gates wrapping seven of them emit no Cypher of their own, so they
add no identity surface.

The `graphowner` gates are wrappers only — `rg -n 'MERGE \(' go/internal/graphowner`
returns nothing outside comments.

### Which node identities those writers actually key on

This is the part an identity cutover needs. Derived by sweeping every node MERGE
in the statement package and tracing each constant back to the writer that
issues it:

```bash
rg -n 'MERGE \(\w+:' go/internal/storage/cypher --glob '!*_test.go'
```

Every node MERGE an unfenced reducer writer performs is keyed on `uid`, with
five exceptions. Named, because a count on its own is not checkable:

| Label | Key | Statement | Writer |
| --- | --- | --- | --- |
| `Repository` | `id` | `canonical.go:131,134`, `canonical_relationships.go:161-256`, `canonical_codeowners_edges.go:33`, `canonical_submodule_edges.go:30-31` | `EdgeWriter` |
| `EvidenceArtifact` | `id` | `canonical_relationships.go:278` | `EdgeWriter` |
| `CloudAction` | `id` | `canonical_invokes_cloud_action_edges.go:20` | `EdgeWriter` |
| `CodeownerTeam` | `ref` | `canonical_codeowners_edges.go:34` | `EdgeWriter` |
| `Environment` | `name` | `canonical_relationships.go:311` and `kubernetes_namespace_node_writer.go:89` | `EdgeWriter` and `KubernetesNamespaceNodeWriter` |

`Environment` is the sharpest one: `CanonicalNodeWriter` and an unfenced reducer
writer MERGE the same label on the same `name` key, so an identity change there
would be fenced on one side and unfenced on the other during the same rollout.

The `uid`-keyed remainder, for completeness: `EdgeWriter` also MERGEs
`DocumentationSection`, `Rationale`, and `ShellCommand`; `SemanticEntityWriter`
MERGEs every semantic entity label on `{uid: row.entity_id}`;
`SecretsIAMGraphWriter` MERGEs four `SecretsIAM*` labels; and
`canonicalGraphWriters` MERGEs `CloudResource`, `KubernetesWorkload`,
`KubernetesNamespace`, `CidrBlock`, `PrefixList`, `SecurityGroupRule`,
`IncidentRoutingEvidence`, `CodeTaintEvidence`, and `ExternalPrincipal`. The
`OrphanSweepStore` MERGEs nothing; it marks and deletes by identity key, which
for its labels is `id`, `path`, or Module's `(name, lang)`.

### Why #6102 is unaffected anyway

No unfenced writer keys a Module node on name. Only two statements write a
`:Module` node at all: `canonicalNodeModuleUpsertCypher`
(`canonical_node_cypher.go:331`), which MERGEs on `{name, lang}` and belongs to
`CanonicalNodeWriter`; and `semanticModuleUpsertCypher`
(`semantic_entity_statements.go:188`), the reducer's, which MERGEs on `{uid}` —
an identity this cutover did not move, and one the uid uniqueness constraint
already records as DDL. The reducer's only other Module-touching path is the
orphan sweep, whose key is already the composite
`(name, coalesce(lang, '<absent>'))`: the identity properties are defined in
`orphanSweepIdentityProperties` (`orphan_sweep.go:400`), the `coalesce`
projection is emitted by `orphanSweepKeyExpr`
(`orphan_sweep_queries.go:67-72`), and `orphan_sweep_test.go:112` pins the
rendered `WITH n.name AS key_0, coalesce(n.lang, '<absent>') AS key_1`.
`EdgeWriter`'s dynamically-labelled statements anchor every endpoint on
`{uid: …}` (`edge_writer_code_call_labels.go:146-178`), and Module is in none of
the four endpoint allowlists (same file, lines 34-62).

An identity cutover on a label a reducer writer does MERGE on would land
squarely in this gap: those pods would be checked at startup and keep writing
through the whole rollout. Whoever writes that change either wires a fence into
the writer or scales the resolution engine to zero — this record exists so the
choice is made rather than assumed.

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

## An absent Module language is not an empty one

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

## Two test-quality findings

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

## Cost of the three follow-up fixes

No-Regression Evidence: none of the three changes adds a round trip or a scan.
The orphan sweep issues exactly the statements it did before — one S1 candidate
read, chunked S2 connectivity reads, a bounded S2 re-verify only on a sweeping
cycle, and up to three key-anchored writes — and the coalesce default change is
one literal inside an expression that was already there, on an already-loaded
candidate row, with the Module statements still anchored on `name` inline in the
MATCH pattern so they resolve through `module_name_lookup` rather than scanning
the label. The schema write fence adds one indexed single-row Postgres read per
fenced writer per 30-second interval (`graph_schema_applications` by backend,
ORDER BY applied_at DESC LIMIT 1), not one per write:
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
