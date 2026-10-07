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
checks one read at the original repository/limit (as of #7250; #7619 now reads
limit+1, see the amendment below) and zero hydration reads for this case. Classification retains the existing artifact-type override and
path predicate. The fixture's late workflow uses a mixed-case workflow artifact
type on `src/file-05001.go`; a `.github/workflows` path would sort early and
would not reproduce the late-candidate case.

Repository story reuses its unfiltered file page after its existing cap+1
sentinel is clipped (#7619 also passes the file sentinel flag forward). Service story independently uses the same 5,000-row
listing loader. HTTP uses the typed summary; stories use a manual map, and MCP
passes through the mounted HTTP handler. All these consumers preserve the
candidate coverage marker.

## Corrected contract

- A page at the limit carries `candidate_pool_status=unknown_at_limit`.
  Reaching exactly 5,000 rows does not establish that more rows exist.
  (Superseded by #7619, see the amendment below: the marker now needs a
  sentinel row past the limit, so an exact 5,000-file repository is complete.)
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

As of #7250 there was no change to SQL, repository selection, file ordering, the
read limit (#7619 later changed the read to limit+1), workflow predicate, graph access, the 20 displayed paths, the 50 hydration
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
  5,000 (as of #7250; #7619 reads limit+1) and zero unnecessary hydration.
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

The blocking Neo4j B-7 gate was initially deferred with Postgres work. A
separately authorized finite full-corpus run passed on 2026-10-01; see
[the full correctness record](7250-neo4j-b7-correctness.md) for exact source,
assertion coverage, resource evidence and three noncomparable timing warnings.
Offline replay did not waive this gate. The full corpus is uncapped and does
not replace the boundary regressions above. Final publication review/gates,
deployed row/value proof, cold/warm p95, owner deployment, and #7250 closure
remain pending. This correctness change still does not fix latency.

## #7619 amendment: exact-limit sentinel

The #7250 rule above keyed the marker on `len(files) >= 5000`, so a repository
with exactly 5,000 files, a complete scan, read `unknown_at_limit`. #7619
replaces that with the cap+1 sentinel the repository story already used
(#7126). The marker is set exactly when the file read returns a row past the
limit.

- `StaticWorkflowArtifactEvidence` (CI/CD HTTP, MCP, and
  `LoadRepositoryScopedCICDEvidence`) reads `ListRepoFiles(..., 5001)`, treats a
  5,001st row as `filesTruncated`, and clips to 5,000 before classification and
  image evidence. Its read cost changes by one extra row on the same ordered,
  repository-scoped read; the SQL is otherwise unchanged.
- `StaticWorkflowArtifactEvidenceFromFiles` and
  `LoadRepositoryScopedCICDEvidenceFromFiles` take the clipped list plus
  `filesTruncated`. The repository story passes the file-read sentinel alone,
  so an entity-only overflow does not make the workflow pool unknown, and it
  still issues one `ListRepoFiles` call (limit 5,001).
- The 4,999, 5,000, and 5,001 boundary is pinned for the direct path, the HTTP
  and MCP handlers, and the story path. The exact-5,000 empty case reads
  `absent` with no marker or reason, the exact-5,000 workflow case reads
  `present` with no marker, and 5,001 files still read `unknown` or `present`
  with the marker. A 5,001-file repository is never reported `absent`.
- The shared golden `testdata/golden/capped-workflow-evidence.json` gained
  `exactly_limit_empty` and `exactly_limit_present`; `capped_present` now uses
  5,001 files with the workflow at ordinal 5,000.

The benchmark tables above predate this change and describe the old `>=` rule;
their "capped" rows are now the `exactly_limit` fixtures, which no longer carry
the marker. They are re-measured in "#7619 response cost" below. The 4,999 / 5,000 / 5,001
boundary is proven by the offline unit, HTTP, MCP and story regressions and the
shared golden. The blocking Neo4j B-7 cell (`corpus-gate (neo4j)`) runs in CI on
the pull request as a no-regression check for uncapped repositories under the
5,001-row read. The corpus has 166 File nodes and does not exercise the boundary,
so B-7 does not prove it. The 2026-10-01 B-7 record stays attributed to its own
commit.

### #7619 response cost

Benchmark Evidence: Go 1.27.1, darwin/arm64, Apple M5 Max. Five alternating
baseline/candidate trials per case, `-benchmem`, `-benchtime=150ms`,
`-count=1`, from compiled test binaries of `origin/main` (8879d632f) and this
branch, on the same 4,999/5,000-file fixtures (the baseline's "capped" fixtures
are the candidate's `exactly_limit` fixtures). The shared host was busy
(`load1` 12 during the run), so these medians disclose cost and do not
establish a speedup. Bytes and allocations are the medians over the five trials;
allocation counts were identical in every trial.

| Static plus story summary | Baseline median | Candidate median | Bytes/allocs before/after |
| --- | ---: | ---: | ---: |
| Uncapped empty (4,999 files) | 92.316 us | 83.969 us | 83,446 / 18 to 83,445 / 18 |
| Exactly 5,000, no workflows | 89.210 us | 85.963 us | 83,544 / 21 to 83,440 / 18 |
| Exactly 5,000, one workflow at ordinal 5,000 | 345.835 us | 316.191 us | 779,229 / 73 to 779,130 / 71 |

The exactly-5,000 rows no longer allocate the coverage marker, which accounts for
the 3 and 2 fewer allocations. The in-memory builder cost is flat within noise.

The direct path now asks the content store for one more row. Read-only paired
`EXPLAIN (ANALYZE, BUFFERS)` of the `ListRepoFiles` statement (`ORDER BY
relative_path LIMIT $2`, custom plan) on the QA reader for a 12,403-file
repository, seven interleaved rounds with alternating first mover: median 12.211
ms at `LIMIT 5000` and 12.532 ms at `LIMIT 5001`, ranges 11.9 to 13.1 ms and
12.0 to 13.3 ms, shared buffers 5,082 versus 5,083. The rounds, in ms, were:

| Round | First mover | `LIMIT 5000` | `LIMIT 5001` |
| ---: | --- | ---: | ---: |
| 1 | 5000 | 12.211 | 12.428 |
| 2 | 5001 | 12.944 | 12.313 |
| 3 | 5000 | 12.192 | 12.965 |
| 4 | 5001 | 12.128 | 12.044 |
| 5 | 5000 | 13.093 | 12.532 |
| 6 | 5001 | 11.897 | 12.580 |
| 7 | 5000 | 12.781 | 13.256 |

The plan is an index scan on `content_files_repo_path_idx` in both. Host `load1` was 18 to 22 during these
reads. Planner statistics for `content_files` were not recorded. This is a
spot check on one repository, not an endpoint p95.

Observability Evidence: the repository story `semantic_overview` stage log now
carries `files_truncated` beside `truncated`, so an operator can tell a file
sentinel from an entity-only overflow. The response carries the
`candidate_pool_status` marker. No new instrument, label, queue, lock or worker.
