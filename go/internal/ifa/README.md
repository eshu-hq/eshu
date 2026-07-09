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
against Ifá's own coverage manifest.

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
- `Catalog`, `CatalogByName`, `CatalogOdu` - the cataloged Odù seed set (see
  `catalog_seed.go`).
- `EnumerateSurfaces`, `OduResolver`, `CoverageInputs`, `RunCoverage` - Ifá's
  own coverage reconciliation, mirroring `go/internal/replaycoverage`'s gate
  shape.

## Dependencies

`ifa` depends on `facts`, `projector`, `replay`, `scope`, `goldengate`,
`relationships`, `replaycoverage`, and `cigates`, plus the SDK's
`sdk/go/collector`, `sdk/go/collector/conformance`, and
`sdk/go/factschema/fixturepack`. It intentionally does not import collector or
parser internals directly — the production extractor and SDK validator are the
only derivation seams into that layer.

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
- `EvidenceSatisfies` checks a correlation's `evidence_kinds` half only. It does
  not check `required_edge_properties` / `allowed_edge_property_values` (e.g.
  rc-29's `source_tool`), because `relationships.EvidenceFact` carries no
  source-tool field and edge-property derivation is reducer-owned
  (post-materialization). The golden-corpus gate asserts that half live;
  extending it to the Ifá contract layer is tracked in #4959.

## Related Docs

- `docs/internal/design/4389-ifa-conformance-platform.md`
- `go/internal/replay/README.md`
- `go/internal/facts/README.md`
- `go/internal/replaycoverage/README.md`
- `go/internal/relationships/README.md`
- `go/internal/goldengate/README.md`
