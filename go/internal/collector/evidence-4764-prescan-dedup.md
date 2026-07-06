# Evidence — #4764 eliminate the duplicate pre_scan parse (php/js/ts/tsx)

Scope: `pre_scan` ran a full tree-sitter parse per file to build `ImportsMap`
(name -> declaring paths), then `parse` re-parsed the same file. For php,
javascript, typescript, and tsx, the ImportsMap-equivalent names are now
derived from the parse-stage phase-1 payload (already computed for every
other purpose) instead of running a second dedicated tree-sitter PreScan pass.
json and groovy are unchanged — they keep their own prescan/parse
relationship and were out of scope (`Engine.preScanOnePath`'s switch already
routes them separately).

The theory (derive-from-parse is output-preserving) was proven BEFORE this
change by a throwaway shim
(`go/internal/parser/prescan_dedup_shim_test.go`, worktree
`4764-prescan-shim`, not committed here): 0/0 symmetric ImportsMap set
difference across php/js/ts/tsx on 7,321 files spanning 6 real repositories
(php-tiny/medium/large, js-small/medium, ts-large).

## Design implemented

1. `go/internal/parser/engine_prescan_derive.go` (new file, extracted from
   `engine.go` to stay under the 500-line cap):
   - `IsDerivedPreScanLanguage(language string) bool` — true only for
     `php`, `javascript`, `typescript`, `tsx`.
   - `DerivePreScanNames(payload map[string]any) []string` — extracts and
     `filepath.Clean`-normalizes names from the `functions`, `classes`,
     `interfaces`, `traits` parse payload buckets, deduplicated. This
     reproduces PHP `class_declaration`/`interface_declaration`/
     `trait_declaration`/`function_definition`/`method_declaration`/
     `anonymous_class` and JS-family `function_declaration`/
     `generator_function_declaration`/`method_definition`/
     `class_declaration`/`abstract_class_declaration`/`interface_declaration`
     plus function-valued `variable_declarator`/`pair`/`assignment_expression`
     — the exact node-kind set `php/prescan.go` and
     `javascript/prescan.go` walk, because the parse-stage main walk already
     visits and buckets all of the same nodes (`php/declarations.go`'s
     `collectPHPFunction`/`collectPHPAnonymousClass`,
     `javascript/javascript_language.go`'s `appendFunctionDeclaration` call
     sites), verified equivalent by the shim.
   - Test-only dispatch counter (`ResetDerivedLanguagePreScanDispatchCountForTest`
     / `DerivedLanguagePreScanDispatchCountForTest`) incremented in
     `engine.go`'s `preScanOnePath` switch, so a regression test can assert
     zero dispatches into the legacy PreScan tree-sitter pass for these
     languages.
2. `go/internal/collector/git_snapshot_prescan_dedup.go` (new file):
   - `partitionPreScanFilesForDerive` splits the pre-scan file set into
     `legacyFiles` (still routed through
     `Engine.PreScanRepositoryPathsWithWorkers`) and `deriveEligibleFiles`
     (php/js/ts/tsx), gated by a caller-supplied
     `deriveFromParseEnabled` bool.
   - `mergeParsedFilesIntoDerivedImportsMap` folds every derive-eligible
     parsed payload's `DerivePreScanNames` output into `importsMap`, keyed by
     the payload's own resolved `path` field.
   - `finalizeDerivedPreScanImportsMap` sorts each name's path list, matching
     `PreScanRepositoryPathsWithWorkers`'s deterministic ordering guarantee.
3. `go/internal/collector/git_snapshot_native.go`'s `SnapshotRepository`:
   `deriveImportsMapFromParse := len(repository.FileTargets) == 0`, then
   partitions `preScanFileSet.Files` accordingly, runs the legacy pre-scan
   pass only over `legacyPreScanFiles`, and — after the parse stage produces
   `parsedFiles` — merges the derived names in and finalizes `ImportsMap`.

### Delta-sync scope decision (explicit)

**Landed: full-ingest dedup only.** The delta-cache described in the issue
(per-file prescan-name cache keyed by `(path, mtime, size)`, mirroring
`config_scope_cache.go`) is **deferred as a follow-up**, not implemented here.

Reason: on a delta sync (`repository.FileTargets` set),
`preScanFileSet.Files` is `parserPreScanFiles(fullParserFiles) ∪
parserPreScanFiles(parserFileSet.Files)` — i.e. pre-scan always covers the
**entire repository** so `ImportsMap` (sent as one complete fact per
generation, see `git_fact_builder.go:143`) stays accurate — while `parse`
(`buildParsedRepositoryFiles`) only visits `parserFileSet.Files`, the changed
targets. Deriving ImportsMap purely from `parsedFiles` on a delta sync would
silently drop every unchanged derive-eligible file's contribution. The gate
`deriveImportsMapFromParse := len(repository.FileTargets) == 0` keeps delta
syncs on the exact legacy code path (verified by
`TestNativeRepositorySnapshotterDeltaSyncKeepsLegacyPreScanForDerivedLanguages`),
so this change carries zero risk to delta-sync correctness. The full-ingest
case (initial clone, full reconciliation) is exactly where `pre_scan`'s
~727s cost was measured, so the dedup lands where it matters most; the
delta-cache is tracked as a follow-up issue.

## Tests (local proof)

- `go/internal/parser/prescan_derive_test.go` (new):
  - `TestDerivePreScanNamesMatchesLegacyPreScan` — for every php/js/ts/tsx
    fixture under `tests/fixtures/ecosystems/` (including the two new
    fixtures added for this issue, `php_comprehensive/anonymous_classes.php`
    and `javascript_comprehensive/pair_assignment_exports.js`),
    `DerivePreScanNames(ParsePath(...))` equals `PreScanRepositoryPaths(...)`
    exactly (0/0 name-set diff).
  - `TestIsDerivedPreScanLanguageScope` — pins the exact language scope
    (php/javascript/typescript/tsx = true; json/groovy/python/go/java =
    false).
- `go/internal/collector/git_snapshot_prescan_dedup_test.go` (new):
  - `TestNativeRepositorySnapshotterDerivesImportsMapWithoutSecondParse` — a
    full-ingest `SnapshotRepository` call over a mixed php/js/ts repo asserts
    `parser.DerivedLanguagePreScanDispatchCountForTest() == 0` (no second
    tree-sitter pass) while `ImportsMap` still contains every expected name,
    including the PHP anonymous-class synthesized name and JS `pair`/
    `module.exports.x =` function-valued exports. Verified this test fails
    (dispatch count = 3) when the dedup gate is disabled, before restoring
    the fix — i.e. it is a real regression guard, not a tautology.
  - `TestNativeRepositorySnapshotterDeltaSyncKeepsLegacyPreScanForDerivedLanguages`
    — a delta sync with one PHP file in `FileTargets` and one PHP file
    outside it asserts the legacy dispatch count is `> 0` and the unchanged
    file's class still appears in `ImportsMap`.
  - `TestMergeParsedFilesIntoDerivedImportsMapSkipsNonDerivedLanguages` — unit
    test proving a non-derive-eligible payload (python) contributes nothing.

### Results

```
cd go && GOCACHE=... go test ./internal/parser/ ./internal/collector/ -count=1
ok   github.com/eshu-hq/eshu/go/internal/parser      ~1-7s
ok   github.com/eshu-hq/eshu/go/internal/collector   ~4-11s
```

Full `./internal/parser/...` and `./internal/collector/...` (all
subpackages, including every `awscloud`/`gcpcloud`/language subpackage) also
pass with zero failures.

`bash scripts/test-verify-golden-corpus-gate.sh` → `pass`.

`make pre-pr` → all local gates passed (gofumpt, golangci-lint whole module
0 issues, `go build ./...`, `go vet ./...`, changed-package tests, 500-line
file cap, package docs, selected exactness + telemetry gates, race lane).

## Performance

Performance Evidence: local equivalence + dispatch-count regression tests
above (0/0 ImportsMap equivalence + proven zero second-parse dispatches for a
full-ingest snapshot), PLUS a remote before/after on the `eshu-remote-validation`
host (Linux amd64, NornicDB `eshu-nornicdb-main:d97f02c1`, filesystem-direct,
`GOMAXPROCS=16`, `ESHU_PARSE_WORKERS=16`).

BEFORE (main, per-repo `pre_scan` stage, observed on the same host's full-corpus
run) vs AFTER (`perf/4764-prescan-dedup`, the same six worst-case repos
bind-mounted, all 6 `source_local` projected):

| repo | pre_scan BEFORE | pre_scan AFTER |
| --- | --- | --- |
| api-php-boatwizardwebsolutions | 141 s | 0.00 s |
| websites-php-youboat | 126 s | 0.03 s |
| wordpress | 58 s | 0.06 s |
| boattrader-legacy | 57 s | 0.00 s |
| search-api-legacy | 48 s | 0.00 s |
| react-native-ui | (parse-bound) | 0.01 s |
| **pre_scan stage sum (6 repos)** | **~458 s** | **0.1 s** |

The `parse` stage is unchanged (212.8 s across the six repos AFTER) — it still
runs; only the redundant second tree-sitter pass in `pre_scan` is removed. The
duplicate parse for php/js/ts/tsx is eliminated (pre_scan → near the
discovery-only floor). On the full reference corpus the ~727 s `pre_scan` stage
sum is dominated by php/js/ts/tsx, so the full-corpus win tracks this collapse.
Result classification: Wall-clock win (removes ~pre_scan/parse-stage duplicate
CPU), output-preserving (0/0 equivalence, review-confirmed).

## Observability

No-Observability-Change: `telemetry.SnapshotStagePreScan`'s
`recordSnapshotStage` call already existed and now additionally logs
`derive_from_parse_file_count`; the stage name, metric, span, and log key set
are unchanged. No new metric or span was introduced.
