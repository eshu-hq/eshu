# #7250 capped workflow coverage truth

This correction preserves the existing repository file read and reports what
its bounded candidate pool can establish. It does not fix endpoint latency or
establish a deployed cold/warm p95. #7250 remains open.

## Reproduction and flow

The production content reader selects the first 5,000 repository file metadata
rows in `relative_path` order. Workflow classification occurs after that cap.
A synthetic repository with 5,001 rows and its sole workflow at ordinal 5,001
therefore produces no workflow matches in the returned page. Before this
change, the static state was `absent` and the empty live summary used
`no_ci_cd_evidence_found`.

The regression enters through `StaticWorkflowArtifactEvidence` and its real
`ListRepoFiles` port, using an ordered fake with no database connection. It
checks one read at the original repository/limit and zero hydration reads for
this case. Classification retains the existing artifact-type override and
path predicate. The fixture's late workflow uses a mixed-case workflow artifact
type on `src/file-05001.go`; a `.github/workflows` path would sort early and
would not reproduce the late-candidate case.

Repository story reuses its unfiltered file page after its existing cap+1
sentinel is clipped. Service story independently uses the same 5,000-row
listing loader. HTTP uses the typed summary; stories use a manual map, and MCP
passes through the mounted HTTP handler. All these consumers preserve the
candidate coverage marker.

## Corrected contract

- A page at the limit carries `candidate_pool_status=unknown_at_limit`.
  Reaching exactly 5,000 rows does not establish that more rows exist.
- Zero observed workflows at the limit means `state=unknown`, with static
  reason `repository_file_scan_limit_reached`. Positive evidence keeps
  `state=present` and its observed count.
- Typed summaries add `static_workflow_coverage_unknown` to `missing_evidence`
  independently of whether live correlations are present, missing, or
  unavailable. Empty live evidence uses that summary reason for an unknown
  static scan. Existing live-present and unavailable reason precedence stays.
- Stories add only the new coverage class on capped pages. Their historical
  omission of other typed missing-evidence classes is preserved.
- Uncapped absence, missing repository scope, nil content store, and read
  failures retain distinct existing states. The marker is omitted when uncapped.

There is no change to SQL, repository selection, file ordering, the read limit,
workflow predicate, graph access, the 20 displayed paths, the 50 hydration
candidates, or hydration worker count. The existing static functions were
split into `workflow_evidence.go` to keep the source file below 500 lines.

## Focused proof

Commands use a worktree-local cache, `GOFLAGS=-p=2`, and `GOMAXPROCS=2`.
All fixtures are synthetic and require no Postgres, graph, Docker, provider
credential, or ops-qa request.

RED/GREEN evidence:

- The original `TestStaticWorkflowCoverageAtFilePageBoundary` and capped
  summary/FromFiles regressions fail on the old implementation, exit 1, and
  pass on the correction, exit 0. The 4,999-row absence control passes on both.
- HTTP and MCP dispatch regressions use the independently authored committed
  `testdata/golden/capped-workflow-evidence.json`. Old production overlays
  fail at 5,000/5,001 and preserve the 4,999 control; corrected production
  passes. Each transport checks the original one file-list read at limit
  5,000 and zero unnecessary hydration.
- Repository story HTTP cap/sentinel cases and repository/service story
  assembly fail on the old production overlay and pass on the correction.
  Service trace preserves the same CI/CD static evidence.
- The committed B-12 HTTP/MCP shapes reject a nonempty candidate marker with
  `state=absent` or summary reason `no_ci_cd_evidence_found`. Seeded
  contradictions are accepted before those shape assertions and rejected
  after them. Honest capped-unknown and uncapped-absent controls pass.
- Existing scoped-grant tests pass, including empty grants and out-of-grant
  repository rejection without store reads.
- A disposable overlay probe compares complete typed and story summary JSON
  before/after for uncapped empty/present static evidence across all three
  live states. All six SHA-256 digests are identical.

The combined package run passed, exit 0:

```bash
GOCACHE="$PWD/.gocache" GOFLAGS=-p=2 GOMAXPROCS=2 go -C go test \
  ./internal/query/repositoryartifacts ./internal/query/cicd \
  ./internal/query/repository ./internal/query/entity ./internal/query \
  ./internal/mcp/cicd ./internal/mcp ./internal/goldengate \
  ./cmd/golden-corpus-gate -count=1
```

Focused race proof passed, exit 0, covering the cap regressions, live-state
matrix, concurrent 50-file hydration counter, and actual HTTP/MCP dispatch:

```bash
GOCACHE="$PWD/.gocache" GOFLAGS=-p=2 GOMAXPROCS=2 go -C go test -race \
  ./internal/query/repositoryartifacts ./internal/query/cicd ./internal/mcp \
  -run 'Test(StaticWorkflowCoverage|CappedEmptyWorkflow|WorkflowCoverage|CICDHTTPWorkflowCoverage|MCPWorkflowCoverage)' \
  -count=1
```

The strict docs build and route/OpenAPI verifier passed. Raw local logs are
retained under `/tmp/eshu-7250-offline-coverage/`, including the initial RED,
transport RED/GREEN, story RED, combined suites, race, access-preservation,
uncapped-wire differential, and paired benchmark logs.

## Local response cost

Benchmark Evidence: Go 1.27.1, darwin/arm64, Apple M5 Max, two Go processors.
Five alternating baseline/candidate trials per case, `-benchmem`,
`-benchtime=150ms`, `-count=1`, on the same 4,999/5,000-file slices. Baseline
production is pinned to `933a72f74edffca455e8d46057b5136416bcf42e` through Go
file overlays; test/benchmark fixtures and inputs are shared across versions.
The shared host was busy. These medians disclose local response cost; they do
not establish a stable speedup, endpoint p95, or deployed no-regression claim.

| Static scan | Baseline median | Candidate median | Allocations before/after |
| --- | ---: | ---: | ---: |
| Uncapped empty | 103.053 us | 105.578 us | 1 / 1 |
| Capped empty | 101.371 us | 100.336 us | 1 / 1 |
| Capped present | 426.009 us | 416.645 us | 52 / 52 |

| Static plus story summary | Baseline median | Candidate median | Allocations before/after |
| --- | ---: | ---: | ---: |
| Uncapped empty | 78.806 us | 81.704 us | 18 / 18 |
| Capped empty | 71.996 us | 75.433 us | 18 / 21 |
| Capped present | 286.612 us | 297.416 us | 71 / 73 |

The story coverage field costs approximately 104 additional bytes / 3
allocations for capped empty and 88 bytes / 2 allocations for capped present
in this measurement. Uncapped allocation counts are unchanged. The static
scan itself adds no allocations. The benchmarked functions are
`StaticWorkflowArtifactEvidenceFromFiles` and
`LoadRepositoryScopedCICDEvidenceFromFiles`; database latency is excluded.

No-Regression Evidence: deterministic fake-port tests preserve read counts,
repository arguments, the 5,000-file candidate bound, sorted 20-path output,
and the existing 50-file hydration ceiling. All six uncapped typed/story JSON
comparisons match the old production implementation. The table discloses the
bounded correctness cost rather than claiming a latency improvement.

Observability Evidence: responses now expose uncertain candidate coverage
instead of repository-wide absence. Existing CI/CD HTTP/query stages and
repository/service CI/CD readback spans/logs are unchanged. There is no new
queue, retry, lock, worker, query, telemetry instrument, or label.

## Golden gate and publication limits

The current B-12 corpus is uncapped. Its new contradiction checks cannot prove
that a capped response emits the marker; the synthetic real-dispatch golden
replay supplies that local proof. Cassette facts, projections, and corpus
counts are unchanged.

The static golden mirror has a preexisting helper-scope defect on the pinned
base: `golden_corpus_git` is sourced inside a staging subshell, then called in
its parent shell. An isolated base run and a candidate run exit 0 while
emitting two `command not found` diagnostics. One additional local run failed
the contaminated staging-identity comparison. These results are not full
pipeline proof and the intermittent mirror failure is not dismissed.

The blocking Neo4j B-7 gate starts Postgres and remains NOT_CHECKED while
Postgres work is deferred. Repository rules require that selected blocking
gate before push. Offline replay does not waive it. Publication, deployed
row/value proof, cold/warm p95, owner deployment, and #7250 closure remain
pending. The arbiter approved the response-only correction and scoped story
wire addition; it explicitly retained this publication gate.
