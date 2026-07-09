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
  `catalog_seed.go` and `roundtrip.go`'s `demoOrgRoundtripOdu`).
- `EnumerateSurfaces`, `OduResolver`, `CoverageInputs`, `RunCoverage` - Ifá's
  own coverage reconciliation, mirroring `go/internal/replaycoverage`'s gate
  shape.

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
- The `ifa-contract-layer` CI gate stays advisory for P1 (the blocking flip is
  a later milestone); `ifa coverage`'s own proof-gate validation surfaces that
  as a `Required` finding without hard-failing the advisory default.
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
- Graph-evidence reach is proven by running the real `relationships.Discover`
  `Evidence` extractor, not a hand-authored classifier. A machine-readable
  fact-kind-to-dispatch surface (e.g. to warn "this Odù carries kind X but X
  never reaches the extractor") is deliberately deferred to a P2+ consumer that
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

## Related Docs

- `docs/internal/design/4389-ifa-conformance-platform.md`
- `go/internal/replay/README.md`
- `go/internal/facts/README.md`
- `go/internal/replaycoverage/README.md`
- `go/internal/relationships/README.md`
- `go/internal/goldengate/README.md`
