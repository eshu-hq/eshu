# ifa

## Purpose

`ifa` is the contract layer for the Ifá conformance platform
([#4393](https://github.com/eshu-hq/eshu/issues/4393),
[#4394](https://github.com/eshu-hq/eshu/issues/4394)). P0 defined an Odù as a
scenario-level set of `facts.Envelope` inputs rendered through the existing
replay canonicalizer. P1 adds the derived expectation and coverage layer on
top: `Derive` computes, for every fact-kind-registry entry, its query-truth
binding, its payload-schema derivation, and its graph-evidence reach, purely
from the registry, the B-12 snapshot, and the replay-coverage manifest —
nothing hand-listed. `RunCoverage` then reconciles those derived surfaces
against Ifá's own coverage manifest. `RoundTripTypedPayloads` adds P1's
terminal proof (issue #4804): that the contract system's typed
`sdk/go/factschema` structs, not only their JSON Schemas, are faithful for a
full fact family, exercised end to end by `odu:demo-org-roundtrip`.

## Ownership Boundary

This package owns contract-seam canonicalization, P1 derivation, and P1
coverage reconciliation. It consumes `facts.Envelope` values directly or
through the same `LoadFacts` shape used by the projector, runs the production
`relationships.DiscoverEvidence` extractor and the SDK's
`conformance.ValidatePayloadSchemas` validator as its only two seams into the
collector/parser and fixture-pack layers, and reuses
`go/internal/replaycoverage`'s `Reconcile`/`BuildReport`/`Findings` machinery
unchanged for coverage bookkeeping. It does not own collector execution,
parser execution, graph writes, reducer scheduling, or fixture-pack schema
authoring; it does not build a second coverage framework.

## Exported Surface

- `Odu` - one scenario-level conformance case.
- `FactLoader` - minimal `LoadFacts` contract matching the projector fact-store
  seam.
- `CanonicalizeOdu` - renders one Odù into replay's deterministic canonical JSON
  form.
- `Derive` - computes `DerivedExpectations` (per-kind `KindExpectation` plus
  B-12 evidence-narrowed `NarrowedCorrelations`) from the fact-kind registry,
  the B-12 snapshot, and the replay-coverage manifest.
- `RepositoryCatalog`, `DiscoveredEvidence`, `EvidenceSatisfies` - the graph
  axis: derive a repository catalog from an Odù's own facts, run the
  production evidence extractor over it, and check a required correlation's
  evidence-kind filter against the result.
- `ValidateOduPayloads` - the payload axis: validate an Odù's facts against the
  fixturepack schema the registry names for their kind.
- `RoundTripTypedPayloads` - the P1 terminal round-trip axis (issue #4804):
  decode every fact in an Odù through its kind's `factschema` `Decode*`
  function and re-encode it, asserting canonical-byte equality with the
  original payload, proving the typed struct neither drops nor reshapes a
  field the collector emitted.
- `Catalog`, `CatalogByName`, `CatalogOdu` - the cataloged Odù seed set (see
  `catalog_seed.go` and `roundtrip.go`'s `demoOrgRoundtripOdu`). The
  `odu:vuln-pack` fixture includes the operator-owned
  `vulnerability.suppression` payload under
  `operator:vulnerability_suppressions`, so Ifá validates the producer's
  server-owned source/author contract separately from provider facts.
- `RepoDependencyBackfillProofOdu` - a lazy, uncataloged retained-shape Odù for
  relationship-backfill SQL and eight-worker storage proofs; it preserves the
  repository-dependency truth cases while adding the measured worst-scope
  source/candidate cardinalities.
- `EnumerateSurfaces`, `OduResolver`, `CoverageInputs`, `RunCoverage` - Ifá's
  own coverage reconciliation, mirroring `go/internal/replaycoverage`'s gate
  shape.
- `MutateCassette`, `MutationKind`, `MutateOptions`, `MutatedFact` (`mutate.go`)
  - the P3 failure-path-determinism fixture generator (ADR step 3a, issue
    #4396): deterministically corrupts K facts of a cassette (selected by
    ascending `StableFactKey`) to produce input that fails typed decode, never
    mutating the source cassette.
- `DeadLetterRecord`, `SortDeadLetterRecords`, `DeadLetterSetsEqual`
  (`dead_letters.go`) - the durable `fact_work_items` dead-letter set shape and
  its cross-run comparator, which `cmd/ifa`'s `ifa dead-letters` verb reads and
  renders.
- **Materialized-edge coverage** (`RegistryMaterializedEdges`,
  `MaterializedEdgeSurfacePrefix`, `MaterializedEdgeOduResolver`,
  `RunMaterializedEdgeCoverage`, `ExpectedEdge`, `LoadExpectedEdges`, and every
  per-family vacuity guard, #5351) moved to the sibling package
  `go/internal/ifa/materializededges` (#6053), to keep this package under the
  repository's directory file-count gate. See that package's `README.md` and
  `doc.go` for the exhaustiveness-gate design, the waiver semantics, every
  family's vacuity guard (including the `handles_route`/`runs_in`/
  `invokes_cloud_action` trio, proven live on both the
  `ifa-determinism`/`ifa-fault-injection` gates with no remaining waiver), and
  the live-gate proof history this section used to carry.
  `cmd/ifa`'s `assert-edges` verb, the `ifa-determinism`/`ifa-fault-injection`
  gates, and every family's Odù catalog entry (still seeded from this
  package's own `catalog_seed.go` and `*_family_catalog.go`/`*_family_odu.go`
  files) are unaffected by the move.

## Dependencies

`ifa` depends on `facts`, `factenvelope`, `projector`, `replay`, `scope`,
`goldengate`, `relationships`, `replaycoverage`, `cigates`, and
`go/internal/synth/gcp`, plus the SDK's `sdk/go/collector`,
`sdk/go/collector/conformance`, `sdk/go/factschema`, and
`sdk/go/factschema/fixturepack`. It intentionally does not import collector or
parser internals directly — the production extractor and SDK validator are the
only derivation seams into that layer. `go/internal/synth/gcp` is
boundary-legal despite living under `go/internal`: it is a synthetic fixture
generator, not a collector, and its own package doc forbids it from importing
`go/internal/collector/...`; it emits every payload through the same typed
`sdk/go/factschema` `Encode*` seam a real collector would use.
`go/internal/factenvelope` is the existing contracts adapter
(`FactSchemaFromInternal`) that maps a durable `facts.Envelope` into the
`factschema.Envelope` shape `Decode*` expects — not a collector or parser
internal either.

## Telemetry

No runtime telemetry is emitted. The package is a pure local conformance
helper with no worker, queue, or deployed-service path.

No-Observability-Change: P1 adds no runtime path, worker, queue, graph write,
or deployed service. Existing diagnostics remain the `go test` suite and
CI-gate selection output; `ifa coverage`'s JSON report and stdout summary are
the P1 operator-facing artifacts.

No-Observability-Change: P3's `MutateCassette` (`mutate.go`) and
`DeadLetterRecord`/`SortDeadLetterRecords`/`DeadLetterSetsEqual`
(`dead_letters.go`) also add no runtime path, worker, queue, or graph write.
`MutateCassette` is a pure in-memory cassette transform (JSON in, JSON out,
no I/O of its own — the CLI wrapper does the disk I/O). The dead-letter
helpers are pure Go values and a comparator; the one Postgres `SELECT` this
slice adds lives in `cmd/ifa/dead_letters.go`, a diagnostic CLI read path, not
a deployed service — see that command's own README "Telemetry" section.
Existing diagnostics remain the `go test` suite (including
`scripts/verify-ifa-dead-letter-determinism.sh`'s docker-backed proof) and the
`ifa mutate-cassette`/`ifa dead-letters` command output.

No-Regression Evidence (#5992): `codeowners_family_catalog.go` adds a compiled
Odù literal and its doc comment mentions the DECLARES_CODEOWNER `MERGE` only
to describe the production Cypher it exercises — it contains no Cypher, worker
claim, batching, or concurrency code of its own; it is a pure fixture that
`TestCodeownersFamilyIsCatalogedAndResolvable` pins against the checked-in
cassette via `reflect.DeepEqual`. The production write path it drives
(`CodeownersOwnershipEdgeMaterializationHandler`) is unchanged by this PR.
No-Observability-Change: same reasoning — no runtime path, worker, queue, or
graph write is added or altered; the observable behavior is entirely in the
already-existing handler and `canonical_codeowners_edges.go`. Both live gates
(`scripts/verify-ifa-fault-injection.sh`'s 21-cell matrix,
`scripts/verify-ifa-determinism.sh`'s N=1/2/4 matrix) were run fresh against
this fixture and matched digests across every codeowners cell and every N,
which is the operator-equivalent proof for a fixture-only change on this
surface.

No-Regression Evidence (#6002): the two changes are different in kind, not
just in family name. `submodule_pin_family_catalog.go` adds
`submodulePinFamilyOdu`, a compiled Odù literal — the binary-portable catalog
representation of the checked-in cassette — with no Cypher, worker claim,
batching, or concurrency code of its own;
`TestSubmodulePinFamilyIsCatalogedAndResolvable` pins it against that cassette
via `reflect.DeepEqual`. `materializededges/materialized_edges_submodule_pin.go` adds
`resolveSubmodulePinMaterializedEdges`, a pure offline vacuity guard: it loads
the hand-derived expected-edge fixture, asserts it covers every relationship
type `submodule_pin_edges`' writer registry accepts, then runs
`reducer.ExtractSubmodulePinEdgeRowsWithQuarantine` — the same pure,
backend-free extraction seam every sibling family's guard uses — and compares
the result to the fixture by exact multiset (`compareSubmodulePinExpectedEdges`),
including each edge's SET-only `pinned_sha`.
It mentions `canonical_submodule_edges.go`'s `MERGE` template only in a
comment, to name the `{path: row.submodule_path}` identity property the
comparison keys on and the separately asserted `pinned_sha`; it contains no
Cypher, graph write, or dispatch code of its own. The production write path
(`SubmodulePinEdgeMaterializationHandler`,
`go/internal/reducer/submodule_pin_materialization.go`, and
`canonical_submodule_edges.go`) is unchanged by this PR.
No-Observability-Change: same reasoning — no runtime path, worker, queue, or
graph write is added or altered; the observable behavior is entirely in the
already-existing handler and `canonical_submodule_edges.go`. The proof helper
now rejects a stale or missing `pinned_sha` in addition to the existing exact
edge identity checks; that diagnostic tightening adds no production telemetry
surface. Current-head live evidence must postdate any expected-property or
fixture change so an older digest-only run is never cited for this stronger
assertion.

## Gotchas / Invariants

- The canonical form is produced by `replay.CanonicalizeValue`, not by a new Ifá
  serializer.
- Facts are cloned before rendering so caller-owned payload maps stay immutable
  after handoff.
- `Work` and `Facts` are mutually exclusive sources for one Odù run; use `Work`
  when validating the durable `FactStore.LoadFacts` seam.
- Expectations are always derived, never hand-listed: the graph axis runs
  `relationships.DiscoverEvidence` for real, and the query axis reads the
  replay-coverage manifest's `read_surface:*` rows rather than string-matching
  a read surface to a query shape.
- Only B-12 required correlations carrying a non-empty `evidence_kinds` filter
  become Ifá `narrowed_correlation:*` surfaces; an unfiltered correlation
  (e.g. rc-19) stays golden-corpus-gate owned and is never an Ifá surface.
- Ifá's own coverage manifest carries bindings (which Odù proves which
  surface), never expected values; see `coverage_falsegreen_test.go` for the
  proof that a wrong-Odù or wrong-correlation binding cannot pass.
- The `ifa-contract-layer` CI gate is CI-blocking as of P4 (#4397). `ifa
  coverage` still defaults to a local advisory report and only hard-fails in
  `-blocking` mode (what `make prove` and CI run); coverage and proof-gate
  findings surface through its `goldengate.Report`.
- `EvidenceSatisfies` checks a correlation's `evidence_kinds` half only, by
  design (#4959 resolved). It does not check `required_edge_properties` /
  `allowed_edge_property_values` (e.g. rc-29's `source_tool`): `source_tool` is
  stamped at materialization time from an edge's primary evidence kind, and
  which facts a resolver aggregates into one edge is undecidable from a fact
  slice pre-materialization. The golden-corpus gate asserts that half live over
  the materialized graph (`goldengate.EvaluateEdgeProperty`), which Ifá's
  post-materialization phases reuse; the one statically decidable half — every
  narrowed rc pins `source_tool` to exactly what its evidence kinds derive to —
  is locked by a reducer-package test (`cross_repo_source_tool_snapshot_test.go`).
- Graph-evidence reach is proven by running the real
  `relationships.DiscoverEvidence` extractor, not a hand-authored classifier. A
  machine-readable fact-kind-to-dispatch surface (e.g. to warn "this Odù carries
  kind X but X never reaches the extractor") is deliberately deferred to a P2+
  consumer that
  needs it (#4959); building it now would mean exporting the dispatch from
  `relationships` and paying its docs-lockstep gates for no in-repo reader.
- `RoundTripTypedPayloads` only proves fact kinds registered in
  `gcpRoundTripByKind` (`roundtrip.go`); a fact kind absent from that table
  fails closed with an error naming the kind rather than silently skipping
  it. The comparator is a direct `replay.CanonicalizeValue` call on each side
  with no extra number-type normalization — proven sufficient for the GCP
  family's int64-typed fields (`gcp_collection_warning.hidden_count`,
  `gcp_dns_record.target_count`/`ttl_seconds`) because `encoding/json` already
  formats a whole-number `float64` identically to an `int64` (see
  `roundtrip_test.go`'s baseline and teeth cases); a future fact family with a
  genuinely divergent number representation would need to prove that
  assumption again before reusing this comparator unchanged.
- A `materialized_edges:<family>` Odù whose facts derive edges correctly under
  the family's PURE extraction seam (e.g.
  `sqlrelationship.ExtractSQLRelationshipRows`) can still be a silent no-op
  against a real graph backend if a required
  endpoint node's containment write never fires. Proven live (#5351,
  `sql_relationship_odu.go`'s `sqlFamilySchemaFileFact`/
  `sqlFamilyGetUserFunctionEntity` doc comments) against a real
  Postgres+NornicDB stack: (1) every `content_entity` entity's graph-node
  write is `UNWIND $rows AS row MATCH (f:File {path: row.file_path}) MERGE
  (n:Label {uid: row.entity_id}) ...` — an inner join — so a `content_entity`
  fact whose `relative_path` names a file with no matching `file` fact
  produces ZERO graph nodes for every entity in that batch, silently, with no
  error and a misleadingly successful projector log line; (2) a graph node's
  actual `uid` is NOT always the content_entity fact's own `entity_id` —
  `projector.canonicalGraphEntityID`'s `canonicalNamePathLineEntityLabels` set
  (includes `Function`, `Class`, ...) IGNORES the incoming id and derives
  `content.CanonicalEntityID(repoID, relativePath, entityType, entityName,
  startLine)` instead, so an edge extractor that reads a hand-picked function
  uid from `parsed_file_data.functions[].uid` must use that SAME precomputed
  canonical value or its edge write's endpoint `MATCH` silently no-ops too.
  Any new `materialized_edges:<family>` Odù whose family writes edges to
  `Function`/`Class`/other `canonicalNamePathLineEntityLabels`-derived
  endpoints must precompute their canonical uid the same way, and every Odù
  carrying `content_entity` facts must also carry a matching `file` fact for
  every `relative_path` it references.
- `MutateCassette`'s two `MutationKind` values do NOT map onto one fixed
  durable outcome. Proven empirically against a real Postgres + NornicDB stack
  (`scripts/verify-ifa-dead-letter-determinism.sh`), not just by reading the
  decode seam: `MutationMissingField` is QUARANTINED per fact
  (`go/internal/reducer/schemadecode/factschema_decode.go`'s `partitionDecodeFailures`) —
  metric + log, no durable `fact_work_items` row. `MutationSchemaMajor`, for a
  fact kind core registers a schema version for, trips the projector's OWN
  admission-time schema-version gate
  (`go/internal/projector/schema_version_admission.go`) BEFORE the reducer's
  typed-decode seam is ever reached — a whole-work-item failure, not a
  per-fact one — and the durable row's `failure_class` came back
  `"projection_bug"` in that run, not the reducer's `"input_invalid"`. Do not
  assume a fixed `failure_class` literal for a mutation kind; compare full
  `DeadLetterRecord` sets with `DeadLetterSetsEqual` instead. See `mutate.go`'s
  `MutationKind` doc comment for the full path-by-path breakdown.

## Layer 3 — load and saturation (P5)

P5 ([#4579](https://github.com/eshu-hq/eshu/issues/4579)) adds the load
vocabulary without inventing a taxonomy. `ScaleSlot` (`slots.go`) adopts
`specs/scale-lab-corpus.v1.yaml`'s corpus slots and binds each to an
amplification fan-out and a `perfcontract` enforcement class — smoke and small
run hermetically, medium and above are operator-gated. A lockstep test asserts
every bound slot id is present in the spec.

`AmplifyAtSlot` (`amplify.go`) is the corpus amplifier: it replays one base Odù
across a slot's disjoint synthetic scopes through the family-native
`synth/gcp.GenerateMultiScope`. It is family-aware by construction and rejects
the generic `scope_id`/`stable_fact_key` rewrite the ADR's Layer 3 landmine
flags as determinism-unsafe (K scopes sharing a payload identity would MERGE
onto one graph node and race last-writer-wins — a false red from the load
generator itself). A family without a disjoint-by-construction generator, or the
schema-only smoke slot, fails closed.

The runtime scenario runners live in sibling subpackages so this core package
stays pure: `go/internal/ifa/throughput` drives an amplified slot through the P2
concurrent driver and proves worker-count-invariant drain hermetically;
`go/internal/ifa/saturation` drives more writes than a permit pool admits and is
the permanent regression for the #3560 dead-letter-flood class (backpressure
engages, work retries, nothing dead-letters, the queue drains to the B-12
residual). Both are registered as the `ifa-load-saturation` CI gate.

## Adding an Odù

A contributor adds a conformance case by dropping a v1 cassette (or a
`LoadFacts`/synth descriptor) and letting expectations derive — there is no
hand-written want-list. Register it in `catalog_seed.go`, bind the surfaces it
proves in `specs/ifa-coverage-manifest.v1.yaml` only once green (C-1), and run
`make prove` to validate coverage and determinism. The full step-by-step
checklist (mirroring the parser package's "add a language" 7-step model) lives
in `AGENTS.md`.

## `--teeth`

Relocated from `scripts/verify-ifa-determinism.sh`, which was at its
500-line cap and needs the room for the families still landing.

Slice scope (#4396 slice 6, "the teeth"): the determinism gate also owns --teeth, the
acceptance clause's negative-path proof that the matrix actually catches "a
deliberately non-idempotent write" instead of passing vacuously. --teeth
builds every host binary with `-tags ifadeterminismteeth`, which links in
exactly one build-tag-gated fault: go/internal/reducer/gcp_resource_
materialization_teeth.go stamps TWO properties onto each CloudResource row —
`ifa_teeth_seq` (a process-global monotonic sequence number, reintroduced in
slice 6b now that the 8-project synth-multiscope cassette the determinism gate
drives — see `SYNTH_MULTISCOPE_PROJECTS` in scripts/verify-ifa-determinism.sh —
makes it interleaving-sensitive again instead of inert) and `ifa_teeth_write_order` (wall-clock
nanoseconds, the guaranteed-red floor) — and go/internal/storage/cypher/
cloud_resource_node_writer_teeth.go appends the two matching SET clauses
that persist them. At least one of those values depends on this run's own
commit/processing order, so it genuinely differs across independent N=1/
N=2/N=4 cells, changing `ifa graph-dump`'s canonical digest and failing the
SAME comparison the gate already runs unmodified. No normal, CI, or
production build ever compiles that fault: cloud_resource_node_writer_
teeth_off.go and gcp_resource_materialization_teeth_off.go (tag
!ifadeterminismteeth) are its zero-cost, zero-behavior default.

## Related Docs

- `docs/internal/design/4389-ifa-conformance-platform.md`
- `go/internal/replay/README.md`
- `go/internal/facts/README.md`
- `go/internal/replaycoverage/README.md`
- `go/internal/relationships/README.md`
- `go/internal/goldengate/README.md`
