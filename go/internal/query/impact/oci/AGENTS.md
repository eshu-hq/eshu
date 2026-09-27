# AGENTS.md - oci

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root, `impact`, or a graph driver.
  `impact` imports this package; reversing that cycles.
- The three Run-calling fetchers and the two bounded Cypher statements
  (`ociTagObservationByRefCypher`, `ociImageByDigestCypher`) stay in
  `go/internal/query/impact/trace_deployment_oci.go`, never here: moving them
  would move the query-source-coverage registry's file key for
  `fetchOCIImageTagRows` / `fetchOCIImagesByDigest`
  (`go/internal/queryplan/testdata/query-source-coverage.yaml`).
- `AdvanceBoundedRead` issues no `Run` call and takes a pre-sorted, pre-
  deduplicated `keys` argument (`SortUniqueStrings`); a caller that skips
  the sort breaks the irreducible-overflow check.
- A withheld image ref never gets a placeholder row in `BuildDigestTruthRows`
  / `BuildTagTruthRows` output; `RegistryTruthLimits` is the only disclosure
  surface.
- Keep one home per symbol: no helper copies across `impact`, `oci`,
  `deployment`, and `querycontract`.

## Verification

Run focused `oci` tests, then `impact`, root `query`, and `queryplan`
suites, plus whole-module build and vet. Run `scripts/verify-package-docs.sh`
whenever this package changes.

## Common changes

- Add a pure OCI registry-truth helper here when it issues no `Run` call;
  wire it into `trace_deployment_oci.go`'s fetchers.
- Export a symbol only when `trace_deployment_oci.go` or a same-package test
  needs it.

## Failure modes

- Importing `impact` or the query root cycles the build.
- Moving a Run-calling fetcher here silently moves the query-source-coverage
  registry's file key and breaks `TestHotCypherManifestCoversEveryProductionQueryCall`.
- Passing an unsorted `keys` slice to `AdvanceBoundedRead` can misclassify a
  continuation as an irreducible overflow or vice versa.

## Anti-patterns

- Do not add a `Run`/`RunSingle` call here; that belongs in
  `trace_deployment_oci.go`.
- Do not duplicate row decoders that already live in `querycontract`.

## ADR-controlled changes

None currently; escalate a proposed change to the fetcher/helper split to an
arbiter model before implementation.
