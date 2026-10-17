# #6695 document/registry package moves

## Scope

Base: `origin/main` at `668f4971c77f` after a fourth conflict-free
rebase (base delta is 2 query-contract files from the #8376 merge, both
file-disjoint from this slice; reversion scan empty). Third rebase base
was `54a73a48c` (7 disjoint docs/specs files). Second rebase base was `7cabdbf0f1e2`
(one digest-block conflict, resolved by re-deriving); first rebase base
was `87f019495`; original base `5e6fa5c5fb29`.

This slice moves four flat collector leaves under plain-English parents,
rename-only via `git mv` (162 renames, 134 of them `.go`):

- `go/internal/collector/mediadoc` into `collector/document/media`
  (package `mediadoc` becomes `media`; the split `sanitize.go` moves
  inside its leaf).
- `go/internal/collector/ocrdoc` into `collector/document/ocr`
  (package `ocrdoc` becomes `ocr`; its `sanitize.go` plus
  `sanitize_test.go` move inside the leaf).
- `go/internal/collector/ociregistry` into `collector/registry/oci`
  (package `ociregistry` becomes `oci`; `ociruntime/` becomes
  `runtime/` with package `runtime`, since `oci/ociruntime` trips the
  directory-stutter gate).
- `go/internal/collector/packageregistry` into
  `collector/registry/package` (package `packageregistry` becomes
  `packages` per the `coordinator/registry/package` precedent;
  `packageruntime/` becomes `runtime/` with package `runtime`).

Two de-stutter renames ride with the move: `artifactory_package_parser.go`
becomes `artifactory_parser.go` (stem repeated the new `package` leaf),
and `runtime_config.go` becomes `config.go` beside the new `runtime/`
sibling (dirgate sibling-shape rule). The replay fault test keeps a
`packageruntime` import alias where the new `runtime` package meets the
existing `sbom/runtime` import; every other caller is repointed by import
path and qualifier only. Exported identifiers, fact kinds, span and
metric names, and envelope shapes are byte-identical.

Out of scope: entrypoint rename, the `terraformstate`/`tfstateruntime`
pair, scanner/OS-vulnerability closure, and every fact-kind, schema,
decode, query, and coordinator-alias contract.

In scope after the head commit: the `specs/live-tests.d` JFrog
scheduled-test row moves one-for-one with the code (old
`packageregistry/packageruntime` path to new
`registry/package/runtime` path) plus its freshly derived digest in
`specs/live-tests.v1.yaml`; the scheduled-test count holds at 373
(the new base added one scheduled row; the digest is freshly derived
over the relocated inventory).

## Inventory

At the base the four leaves held 9, 8, 62, and 55 files respectively
(AGENTS.md/README.md/doc.go trios included). The destinations hold those
same file names except the two de-stutter renames above. Non-test `.go`
counts after the move: `document/media` 5, `document/ocr` 4,
`registry/oci` 6, `registry/package` 21, `registry/oci/runtime` 10,
`registry/package/runtime` 9 — every directory stays under the 40-file
cap, and no new directory repeats its parent name. New trios:
`collector/registry/{doc.go,README.md,AGENTS.md}` (documentation-only
namespace); the `document/` parent trio already existed and now records
its landed `media`/`ocr` children.

Direct callers repointed in the same change: the `collector-oci-registry`,
`collector-package-registry`, and `collector-sbom-attestation` commands,
`collector/sbom/runtime`, four `vulnerabilityintelligence` files, the two
coordinator planners, the replay inputtape fault test, the
contract-encode adoption ratchet, wrapcheck paths, telemetry-coverage
rows, live-smoke and verification-gate commands, the surface-inventory
YAML plus regenerated JSON, and the 4786 matrix. Dated evidence notes
that quote vacated paths as they stood take moved-file-refs allowlist
entries; live pointers are repointed.

## Proof

All commands run with `GOTOOLCHAIN=go1.26.9 CGO_CFLAGS=-std=gnu11` from
`go/` unless noted, backend unit (in-memory, no Docker):

- `go build ./internal/collector/document/...`: exit 0.
- `go vet ./internal/collector/document/...`: exit 0.
- `go test -count=1 ./internal/collector/document/...`: `export`,
  `media`, `ocr` ok.
- `go test -count=1 ./internal/collector/ ./internal/collector/document/...`:
  `collector` ok (contract-encode ratchet included).
- `go build ./internal/collector/registry/...` plus sbom runtime,
  vulnerabilityintelligence, coordinator, the three commands, and replay
  inputtape: exit 0.
- `go vet` over the same set plus `query/package/...`,
  `reducer/awscloud`, `projector/canonical`: exit 0.
- `go test -count=1 ./internal/collector/registry/...`: all 12
  packages ok; `go test -list '.*'` discovers 180 tests in the moved
  tree.
- `go test -count=1` over the collector root, sbom runtime,
  vulnerabilityintelligence, coordinator tree, the three commands, and
  replay inputtape: all ok.
- `bash scripts/verify-moved-file-refs.sh`: all cases passed
  (the JFrog row repoint is in place, so no exemption remains).
- `bash scripts/verify-doc-citations.sh`, `verify-docs-refs.sh`,
  `verify-filename-stutter.sh`, `verify-package-docs.sh`,
  `verify-telemetry-coverage.sh`, `verify-license-header.sh`,
  `capability-inventory -mode verify`, `git diff --check`: all pass.
- `bash scripts/verify-live-tests-ledger.sh`: RED/GREEN pair passed
  on this head; the ledger names the moved
  `registry/package/runtime/jfrog_live_test.go` at its relocated row.

## No-Regression Evidence (#6695):

`go test -count=1 ./internal/collector/registry/...` passes all 12
moved packages (180 discovered tests) and every direct consumer suite
stays green on Go 1.26.9 linux/amd64 with the unit backend and no
Docker. Input shape is unchanged: the move renames packages and paths
only, so the same test binaries exercise the same assertions at the new
import paths; the contract-encode ratchet pins identical envelope
builders at the new relative paths. No benchmark, queue, lease, or
concurrency behavior changes, so no timing comparison applies.

## No-Observability-Change (#6695):

No metric, span, or log was added or renamed. The OCI and
package-registry telemetry rows keep their existing instruments with
path-only repoints, confirmed by `verify-telemetry-coverage.sh`
(instruments and doc agree, no new untracked stages). Fact kinds,
envelope shapes, and read surfaces are untouched.
