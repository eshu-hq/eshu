# AGENTS.md - impacttrace

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root, `impact`, or graph drivers.
  `impact` imports this package; reversing that cycles.
- `BuildDeploymentTraceResponse` requires a non-nil caller-built overview
  map; counts attach in place. Derive only what shared `querycontract`
  helpers yield identically to the service-story builder; evidence lists
  attach only when absent; `deployment_truth_tier` stays caller-owned.
- Keep one home per symbol: no helper copies across `impact`, `impacttrace`,
  `querycontract`, and `querytestutil`.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext`, never hand-built to widen access.

## Verification

Run focused `impacttrace` tests, then `impact`, root `query`, `queryplan`,
and `mcp` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves code,
never Cypher text or queue/projection behavior.

## Common changes

- Add a pure family helper here when it touches no handler state; wire it
  into `impact` handlers or `BuildDeploymentTraceResponse` fields.
- Export a symbol only when `impact`, a staying root test, or an external
  caller pins it; document who pins it on the export.

## Failure modes

- Importing `impact` or the query root cycles the build.
- Rebuilding service-story shaping here duplicates root logic that will
  drift; derive only through shared helpers.
- Attaching caller-owned keys unconditionally (normalized `api_surface`,
  `deployment_truth_tier`) overwrites production values.

## Anti-patterns

- Do not add handler orchestration, routes, or `ImpactHandler` methods here.
- Do not duplicate test fakes or row decoders that already live in
  `querytestutil` or `querycontract`.
- Do not expose graph or Postgres implementations through the helpers.

## ADR-controlled changes

Changing the overview ownership split, adding a graph driver import, or
moving route assembly into this package requires an accepted architecture
decision before implementation.
