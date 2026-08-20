# AGENTS.md - internal/ifa/materializededges guidance

## Read first

1. `README.md` - package purpose, ownership boundary, exported surface.
2. `doc.go` - what a vacuity guard proves (and deliberately does not), and
   why the package boundary sits where it does.
3. `materialized_edges.go` - `MaterializedEdgeOduResolver.Resolve`'s dispatch
   switch; read this before adding a new family.
4. The `go/internal/ifa` root `AGENTS.md` and `README.md` - the parent
   package's Odù catalog, coverage manifest, and CI gate contract this
   package plugs into. Everything there still applies here.

## Invariants

- A guard's registration point is `MaterializedEdgeOduResolver.Resolve`'s
  `switch family` statement (`materialized_edges.go`), not a package
  `init()`. Adding a new family's guard function without adding its `case`
  leaves every coverage-manifest row naming it unresolvable through the
  "no vacuity guard registered" default branch — the exact #5993 shape.
- A guard proves the extractor against a hand-derived expected-edge-set
  fixture, run over the SAME production seam the reducer uses. Never
  hand-author the extractor's OUTPUT to make a guard pass; if the extractor's
  real output does not match the fixture, either the extractor or the
  fixture is wrong, and the guard existing to say so is the point.
- This package imports `ifa`; `ifa`'s production code (non-test) must never
  import this package back. The Odù catalog and per-family fixture builders
  stay in `ifa` specifically because `ifa/catalog_seed.go` calls them at
  registration time — moving a `*_family_catalog.go` file here would create
  that production cycle. If a change seems to need it, it does not; find the
  seam through an exported `ifa` accessor instead (see the next point).
- When a moved test needs something from `ifa` that is unexported:
  - **Pure constant** (a plain string/int literal with no logic): duplicate
    it locally, with a doc comment naming the `ifa` source, why it could not
    be exported-and-imported instead (usually: the original is still needed
    unexported by other `ifa` files), and what must stay in sync. This
    package already carries examples in every family guard file — follow
    their shape.
  - **Loader, builder, or assertion logic** (anything with a body beyond
    returning a literal): export it from `ifa` and reference it as
    `ifa.ExportedName`. Do NOT duplicate the body. A second copy of a
    cassette decoder or catalog-fact builder drifts from the original
    silently — exactly the false-green class this whole package exists to
    prevent. Give the newly-exported identifier a real godoc comment, not a
    restatement of its name; see the codeowners/rationale/code-calls/
    documentation/deployable-unit family files in `ifa` for the pattern
    (each names #6053 and explains which moved test needs it).
- A staying `ifa` test that spans multiple families (or a fixture outside
  this package's family list, like the `gcpcloud` cassette
  `TestIFALiveMatrixGenerationIDsAreUniqueAcrossScopes` also reads) cannot
  move here even though it touches a moved guard's cassette — it does not
  belong to any single family. Such a test duplicates the one or two small
  pure identifiers it needs (see `ifa/live_matrix_generation_identity_test.go`)
  rather than importing this package.
- `repoRootDir` (this package's test helper) walks up FOUR directories, not
  three: this package sits one level deeper than `ifa`
  (`go/internal/ifa/materializededges/` vs `go/internal/ifa/`). If you copy a
  repo-root-relative helper from `ifa` into this package, adjust the hop
  count — the file will compile with the wrong count, it will just resolve
  fixtures to the wrong directory and fail confusingly.

## Common changes

- **Adding a new materialized-edge family**: add the family's guard file
  here (`materialized_edges_<family>.go`) following an existing family's
  shape (load fixture -> require registry-type coverage -> run the real
  extractor -> compare exactly), add its `case` to
  `MaterializedEdgeOduResolver.Resolve`, add the family's manifest row and
  scenario requirements, and update `specs/ci-gates.v1.yaml`'s
  `ifa-determinism`/`ifa-fault-injection` sourced-file globs if they are
  file-path-scoped rather than directory-scoped.
- **A family's fixture drifted from its cassette**: check whether the
  fixture builder (`ifa/*_family_catalog.go`) or the committed cassette
  (`testdata/cassettes/<family>/...`) changed; the lockstep test
  (`Test<Family>FamilyCassetteMatchesCompiledCatalog` or similar, moved here
  with its guard) exists specifically to catch a one-sided edit.

## Failure modes

- A coverage-manifest row resolves "no vacuity guard registered" for a
  family that clearly has one: check `Resolve`'s `switch` for a missing
  `case`, not the guard function itself.
- A guard passes locally but the live gate (`ifa-determinism` /
  `ifa-fault-injection`) fails on the same family: the guard proves the
  extractor, not the live MERGE write — check for a missing endpoint node on
  the live graph before assuming the guard's exact-set comparison is wrong.
- `go test ./internal/ifa/materializededges/...` passes but CI never ran it.
  TWO different wirings can cause this and they fail differently:
  1. `ifa-contract-layer` and `ifa-materialized-edge-coverage` run PACKAGE-EXACT
     `go test` commands (`./internal/ifa ./internal/ifa/materializededges
     ./cmd/ifa`). A package missing from that list is simply not run, and every
     gate still reports green. Note the command is ALSO hardcoded in
     `.github/workflows/static-contract-gates.yml` -- editing only the registry
     changes local behaviour and nothing in CI.
  2. `ifa-determinism` and `ifa-fault-injection` are the live gates; their local
     commands are the static mirrors (`scripts/test-verify-ifa-*.sh`), and what
     matters for them is the SOURCED GLOBS plus this package appearing in
     `.github/workflows/ifa-determinism-gate.yml`'s `on.paths`. A registry
     trigger with no matching workflow path selects the gate as blocking and
     GitHub then never starts it -- the determinism mirror's
     registry-subset-of-workflow loop is the only check that catches it.

## Do not change without ADR review

- The package boundary itself (moving a family guard back into `ifa`, or
  moving a `*_family_catalog.go`/`*_family_odu.go` fixture builder here) --
  #6053's two proofs (the in-package-test import cycle, and the
  `catalog_seed.go` production cycle) are why the boundary sits exactly
  here; re-litigating it needs the same rigor those proofs used, not
  intuition.
- The waiver semantics in `materialized_edges.go`
  (`materializedEdgeWaiverProofGateFor`, `materializedEdgeWaiversByKey`,
  the SQL-family delta-live unwaivable carve-out) without reading
  `RunMaterializedEdgeCoverage`'s full doc comment first -- the
  (surface, proof_gate) key shape is deliberate and easy to accidentally
  break by keying on surface alone.

## Verification

```bash
cd go && go build ./internal/ifa/...
cd go && go test ./internal/ifa/... ./cmd/ifa -count=1
bash scripts/dev/precommit-go.sh dirgate-all
bash scripts/verify-package-docs.sh
```
