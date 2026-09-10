# #6627 projector code-intent package move

## Scope and classification

This change relocates and renames three in-process projector intent packages:
`codefunctionsummary` to `code/function/summary`, `codeinterprocevidence` to
`code/interproc/evidence`, and `codetaintevidence` to `code/taint/evidence`.
It changes package declarations, import paths and aliases, exported builder
names, private helper names, filenames, tests, and documentation. It does not
change an executable branch, lookup, decode call, reducer-intent field, loop,
allocation strategy, I/O operation, backend query, worker, lease, retry, batch,
queue write, or graph write. This is not a performance improvement and is not
evidence that the packages are independently extractable.

## Proof environment

| Item | Baseline | After |
| --- | --- | --- |
| Source | `origin/main` at `ea0c2e1a903f862b75c6dcd75e3854b50737f256` | Code-bearing commit `5fd8e4de8158526c0151055692062728f4fffeb2`; this evidence note is a documentation-only follow-up |
| Go | `go1.26.6 linux/amd64` | `go1.26.6 linux/amd64` |
| Backend/version | None; in-memory builder and AST contract proof, with no Postgres, NornicDB, or Neo4j connection | Same |
| Runtime measurement | Not run; no wall-clock, throughput, CPU, or allocation claim | Not run; no wall-clock, throughput, CPU, or allocation claim |

## No-regression proof

No-Regression Evidence: the same logical input matrix ran before and after:
empty and no-trigger generations; valid finding-only facts; marker-only
reconciliation; a finding plus an earlier marker to pin finding precedence;
function-summary decode failure and marker repo-ID fallback; whitespace-padded
`CollectorKind` with a conflicting `SourceRef`; and an interproc non-triggering
function-summary fact. The baseline old-package suite completed 20 of 20
matched tests with exit 0, and the after suite completed 20 of 20 matched tests
with exit 0.

The root mixed-fanout fixture completed with exit 0 before and after. Its
assertions pin 42 emitted domain intents across 44 builder probes, exact domain
order, and the full `FactID`, `EntityKey`, `Reason`, `SourceSystem`, and
`Payload` values. Each moved builder still terminates with either zero intents
for a non-trigger or one scope-generation intent for its domain. Queue rows,
storage rows, and graph rows are not applicable: this slice neither executes
nor changes enqueue, storage, or graph work, and no backend was started.

The following commands ran from each checkout's `go/` directory. Host-local
cache paths are intentionally omitted.

```bash
GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 20 \
  '^TestBuild(CodeFunctionSummary|CodeInterprocEvidence|CodeTaintEvidence)ReducerIntent' -- \
  ./internal/projector/codefunctionsummary \
  ./internal/projector/codeinterprocevidence \
  ./internal/projector/codetaintevidence -count=1

GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 20 \
  '^TestBuildReducerIntent' -- \
  ./internal/projector/code/function/summary \
  ./internal/projector/code/interproc/evidence \
  ./internal/projector/code/taint/evidence -count=1

GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 2 \
  '^(TestAppendScopeGenerationReducerIntentsFanOutParity|TestReducerIntentProbeCountMatchesDocumentedCount)$' -- \
  ./internal/projector -count=1
```

All four baseline/after invocations exited 0. Production source was also
compared after deterministic path, package, exported-symbol, and private-helper
substitution; the normalized diff was empty. The B-7 cassette tree and B-12
golden snapshot are byte-identical between baseline and the code-bearing
commit.

The change is safe because root still constructs one immutable `FactLookup`,
invokes the same 44 probes in the same sequence, and sorts the assembled values
before enqueue. The code-domain positions remain taint, interproc, then function
summary. Trigger priority, marker fallback, exact reducer domains, entity keys,
reasons, fact IDs, source labels, function-summary payload, and typed SDK
decode/error handling are unchanged. The leaves still import internal facts,
projector-intent, and reducer contracts; this is an in-process ownership cleanup,
not a service boundary.

## Observability

No-Observability-Change: no metric instrument or label, span, structured-log
message or field, status field, queue table, failure class, route, or runtime
setting is added, removed, or renamed. No runtime telemetry sample, log record,
or status row was collected because this proof is intentionally backend-free
and the executable behavior is unchanged.

Operators retain `eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds` on the projector side. The three
reducer domains retain `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`; malformed reducer inputs retain
`eshu_dp_reducer_input_invalid_facts_total`, the `reducer input_invalid fact
quarantined` structured log, and the existing durable work-item status and
failure fields. The moved pure builders emit no signal themselves. The updated
rows in `docs/public/observability/telemetry-coverage.md` point at the new source
paths.
