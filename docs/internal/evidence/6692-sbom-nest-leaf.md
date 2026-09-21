# #6692 sbom nest leaf move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

- `go/internal/collector/sbomdocument/` → `go/internal/collector/sbom/document/`
  (`package sbomdocument` → `package document`, incl. the external
  `sbomdocument_test` → `document_test`).
- `go/internal/collector/sbomruntime/` → `go/internal/collector/sbom/runtime/`
  (`package sbomruntime` → `package runtime`; no stdlib `runtime` import
  in either leaf, and no importer imports stdlib `runtime` alongside —
  verified by census, so no qualifier collision).
- Importers repointed by import path + qualifier only (9 files):
  collector-sbom-attestation cmd (service, config, tests),
  reducer/sbomattest (2 tests, incl. a functional `os.ReadFile` fixture
  path), reducer/containerimage test (2 comments), collector
  emit_bench_test (comment), inputtape fault test; plus the
  document↔runtime cross-import and fixture paths inside the leaves.
- Preserved byte-identically (contracts, not paths): fact kinds, span/
  metric names, envelope shapes, tape/cassette names.
- Leaf trios updated (README titles/paths/perf commands, AGENTS headers);
  path-only updates to the surface-inventory overlay (+ regenerated
  JSON), fact-kind-registry comment, telemetry-coverage rows,
  evidence-and-supply-chain proof command, 4786 matrix collector cells,
  cmd AGENTS, sdk README.
- Historical run records untouched (5456 evidence, 4786 prose history).
- The `wrapcheck` lint exclusion for the old runtime path moves with the
  code to `internal/collector/sbom/runtime/`.
- No dirgate row covers either subtree (both under the 40 cap).

## No-Regression Evidence:

Baseline: the pre-move file contents at origin/main 3fbf13ea6 (git
records the moves at high rename similarity; code edits are the package
clauses, importer paths/qualifiers, fixture paths, and gofumpt import
re-sorting only). After: `go test -count=1
./internal/collector/sbom/... ./cmd/collector-sbom-attestation/
./internal/reducer/sbomattest/ ./internal/reducer/containerimage/`
green on the post-move tree. Backend/version: unit-level proof only
(fixture/fake-based suites). Input shape: unchanged collector configs
and envelope inputs. Why safe: the move is invisible at runtime (same
package behavior, same contracts); the only production change is the
import path.

## No-Observability-Change:

No spans, metrics, structured logs, or status surfaces are added,
removed, renamed, or re-labeled: the telemetry-coverage rows keep their
exact names (path cells only).

## Proof (this worktree, before push)

- `go build` on the sbom tree, cmd, and all importer trees clean.
- `go test` green: sbom/document, sbom/runtime, cmd, sbomattest,
  containerimage.
- `gofumpt -l` clean on all touched Go files; every touched file < 500 lines.
