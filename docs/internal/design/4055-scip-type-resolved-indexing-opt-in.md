# SCIP/type-resolved indexing opt-in design (#4055)

## Status

Accepted for an opt-in proof slice. SCIP remains supplemental evidence and is
not a default parser mode.

## Acceptance criteria restated

- Produce a design note with a per-language recommendation: implement now,
  defer, or reject with rationale.
- Include a fixture-backed prototype or spike for at least one high-value
  language pair if adoption is recommended.
- Preserve no-provider deterministic parsing and relationship answers.
- Require no semantic or LLM provider key for SCIP/type-indexed evidence.
- Keep public parity claims explicit about tree-sitter-only, type-resolved, and
  unsupported modes.

## Current Eshu support

Eshu already has a SCIP ingestion path:

- `go/internal/parser/scip_support.go` maps selected extensions to external
  indexers: `scip-python`, `scip-typescript`, `scip-go`, `scip-rust`,
  `scip-java`, and `scip-clang`.
- `go/internal/parser/scip_parser.go` decodes `index.scip` protobuf files and
  emits supplemental definitions plus `function_calls_scip` rows.
- `go/internal/collector/git_snapshot_scip.go` groups selected files by
  language and package/workspace root, runs bounded indexer workers, parses
  SCIP output, and merges only SCIP call facts into native parser payloads.
- `go/internal/reducer/code_call_materialization_index.go` and
  `code_call_materialization_symbol_index.go` materialize SCIP rows with
  `resolution_method=scip` and do not collapse them into generic inferred
  relationships.

The gap was policy, not raw capability: before this note, collector
configuration enabled SCIP by default when binaries were present. That violated
the deterministic no-provider default. This slice changes `SCIP_INDEXER` to an
affirmative opt-in gate (`1`, `true`, `yes`, or `on`) and keeps unset,
unrecognized, and false-like values on native parsing.

## Decision

Adopt SCIP as an opt-in, bounded, clearly labeled type-resolution supplement.
Do not make SCIP default-on. Do not replace tree-sitter parser facts. Do not
require provider keys. Missing binaries, indexer failures, parse failures,
empty index output, and files omitted from `index.scip` all fall back to native
parser output while emitting bounded SCIP attempt telemetry.

SCIP output may add `function_calls_scip` and source-backed `scip_symbol`
identity. Native parser buckets remain authoritative for discovery coverage,
content entities, and deterministic no-provider answers.

## Per-language recommendations

| Language family | Recommendation | Rationale |
| --- | --- | --- |
| Go | Implement now, opt-in | Local support maps `.go` to `scip-go`; native Go already emits stable `scip-go gomod` symbols when package identity is known. SCIP can improve cross-package call binding without replacing native parser facts. |
| TypeScript / JavaScript | Implement now, opt-in | Local support maps `.ts`, `.tsx`, `.js`, and `.jsx` to `scip-typescript`. Use for package/workspace call precision and cross-file symbol identity; keep framework-route parity under #4039/#4046. |
| Python | Implement now, opt-in | Local support maps `.py` and notebooks to `scip-python`. Dynamic imports and plugin loading stay ambiguous unless positive SCIP/parser evidence exists. |
| Rust | Defer | Local support maps `.rs` to `scip-rust`, but crate feature/cfg and macro behavior need fixture-backed language parity proof before public type-resolved claims. |
| Java | Defer | Local support maps `.java` to `scip-java`, but JVM build/source-set behavior and package API reachability need fixture proof alongside #4040. |
| Kotlin / Scala JVM | Defer | No local SCIP extension map exists for Kotlin/Scala today. Keep existing tree-sitter and reducer evidence claims under language parity issues until a JVM-wide design proves source-set, generated-code, and package identity handling. |
| C / C++ | Defer | Local support maps C/C++ extensions to `scip-clang`, but compile database and include-path availability dominate correctness. Require a build-context design before claiming type-resolved C/C++. |
| C# / .NET | Defer | No local SCIP extension map exists. Roslyn/source generator behavior needs a separate .NET indexing design and parity issue before adoption. |
| Long-tail languages | Reject default adoption | Only adopt per language after a maintained indexer, bounded runtime cost, fixtures, and public support wording exist. Tree-sitter-only remains the honest default. |

Sources reviewed for indexer availability and risk:

- SCIP protocol: https://github.com/sourcegraph/scip
- Go indexer: https://github.com/sourcegraph/scip-go
- TypeScript/JavaScript indexer: https://github.com/sourcegraph/scip-typescript
- Python indexer: https://github.com/sourcegraph/scip-python
- Java indexer: https://github.com/sourcegraph/scip-java
- Rust indexer: https://github.com/sourcegraph/scip-rust

## Dedupe boundaries

- #4055 owns SCIP/type-resolved indexing research, the opt-in gate, and
  type-resolved support labels.
- #4056 owns API/MCP workflows for PDG, taint, reaching definitions, and CFG.
  This design does not add those code-flow APIs.
- #4037 and child language parity issues own tree-sitter/native parser parity
  claims. SCIP cannot mark a language parity issue complete unless the issue's
  own fixture, reducer, and query evidence is satisfied.
- #4049 owns cross-repo relationship tools. SCIP may improve evidence quality
  for those tools, but it is not their public workflow contract.

## Merge model and public labels

Public docs and API/MCP surfaces should distinguish:

- `tree_sitter_only`: deterministic native parser evidence, default mode.
- `type_resolved`: explicit SCIP supplement was enabled and used for the row or
  answer, labeled by `resolution_method=scip` or equivalent provenance.
- `unsupported`: no maintained indexer or no adopted Eshu contract.
- `unavailable`: the language is configured for SCIP but the binary, project
  context, or index output was unavailable.
- `ambiguous`: positive evidence did not disambiguate the target.

SCIP rows are additive evidence. They must not delete native rows, lower
deterministic confidence, or convert absent evidence into a negative claim.

## Prototype proof slice

This PR keeps the existing SCIP ingestion implementation and makes its runtime
policy opt-in. The fixture-backed slice covers the high-value Python and Go
pair in one mixed repository:

- `TestSCIPSnapshotRunsEachSupportedLanguageSubtree` proves an explicitly
  enabled snapshot runs SCIP for Python and Go subtrees, merges each language's
  supplement, and preserves native parsing around it.
- `TestSCIPSnapshotKeepsSelectedFilesMissingFromIndex` proves selected Python
  files omitted from `index.scip` still parse natively.
- `TestLoadSnapshotSCIPConfigDefaultsDisabledForTopLanguageList` and
  `TestBuildBootstrapCollectorWiresDefaultSCIPDisabled` prove native-only
  parsing is the default in collector and bootstrap wiring.

## Runtime and observability

Default mode now skips external SCIP process execution entirely. When explicitly
enabled, `SCIP_WORKERS` bounds concurrent indexer processes across repository
snapshots and `SCIP_LANGUAGES` can narrow the language allowlist. Operators can
observe SCIP behavior through existing signals:

- `eshu_dp_scip_snapshot_attempts_total{language,result}`
- `eshu_dp_scip_process_wait_seconds{language}`
- existing collector parse-stage logs and file parse metrics

No new metric, span, status field, worker, queue, graph write, or provider
runtime is introduced by this slice.

Performance Evidence: baseline behavior before this slice enabled SCIP whenever
`SCIP_INDEXER` was unset and an allowed language plus matching external binary
were available, so a default Python/Go repository could enter the shared SCIP
process limiter and launch compiler-grade indexer work during the collector
parse stage. After this slice, unset `SCIP_INDEXER` returns `Enabled=false` in
`LoadSnapshotSCIPConfig`; `trySCIPSnapshot` records a single
`eshu_dp_scip_snapshot_attempts_total{language="unknown",result="disabled"}`
row and immediately returns to native parser workers without binary lookup,
process-limiter wait, indexer execution, protobuf parsing, graph writes, queue
items, or extra rows. Backend/version: local Go test runtime for this branch,
with no Postgres, NornicDB, Neo4j, or provider key required. Input shape:
fixture Python and mixed Python/Go repositories in the collector SCIP tests.
Terminal counts: native parse still emits one parsed file per selected input;
explicit SCIP tests still emit one SCIP supplement for indexed files, and
missing `index.scip` files remain native-only. After measurement:
`go test ./internal/collector -run
'Test(LoadSnapshotSCIPConfig|SCIPSnapshot|SCIPLanguage|SCIPWorkers)' -count=1`
passed on the rebased branch and proves default-off, explicit-on, binary
fallback, subtree fan-out, missing-index fallback, and worker-limit behavior.

Observability Evidence: the default-off path reuses the existing SCIP attempt
counter with `result="disabled"`; the opt-in path continues to use
`eshu_dp_scip_snapshot_attempts_total{language,result}`,
`eshu_dp_scip_process_wait_seconds{language}`, collector parse-stage logs, and
file parse metrics. No-Observability-Change: this slice adds no metric name,
metric label, span, status field, log field, worker, queue, graph write,
runtime endpoint, deployment profile, or provider configuration.

## Verification

Focused gates for this slice:

```bash
cd go && go test ./internal/collector -run 'Test(LoadSnapshotSCIPConfig|SCIPSnapshot|SCIPLanguage|SCIPWorkers)' -count=1
cd go && go test ./cmd/bootstrap-index -run 'TestBuildBootstrapCollectorWires.*SCIP' -count=1
```

Docs and hygiene gates:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
