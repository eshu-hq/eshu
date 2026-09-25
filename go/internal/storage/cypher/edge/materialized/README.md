# Materialized-edge registries (storage/cypher/edge/materialized)

Data-only registries the Ifá exhaustiveness gates read to scope
materialized-edge assertions per family. See `doc.go` for the package
contract.

## Ownership

- Owner: Ifá materialized-edge coverage (`internal/ifa/materializededges`,
  `cmd/ifa` assert wiring, `internal/query/querycontract`).
- Inputs: none at runtime; the registries are compiled-in maps.
- Outputs: edge-type sets, endpoint labels, identity properties, and the
  repo-dependency split-retract builder.

## Dependencies

- Parent `cypher` package: canonical statement templates and builders.
- `edge/writer` sibling: shell-exec templates and the split-retract
  builder shared with the live retract path.
- Never imported by the parent `cypher` package.

## Change guidance

- Registry reason strings must cite the exact template or retract constant
  (`TestRegistryReasonsCiteRealSymbols` guards this).
- Adding a type to a family requires the same addition in the writer that
  emits it; the registry never invents coverage the writer lacks.
- Set `OneEdgePerEndpointPair` on an endpoint constraint only when every
  writer of that type MERGEs one shared canonical edge per endpoint pair
  (RUNS_ON, #6671). `assert-edges` then counts that pair's multiplicity across
  all `evidence_source` stamps. If distinct stamps are distinct edges by
  design, leave it false.
- Keep the repo-dependency alternation derived, not relisted
  (`TestRepoDependencyRegistryDerivesTheAlternationRatherThanRelistingIt`).
