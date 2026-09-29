# #7246 change-planning coverage truth

The change-planning responses propagate known topic-pool and changed-path
symbol caps through change-surface, pre-change, and developer-change-plan
coverage. An empty nonzero-offset topic page reports
`candidate_pool_status=unknown_empty_page`; this describes uncertainty from the
authorized page only. Scoped requests with no granted repositories skip the
topic content-store read. For nonempty grants, the production content reader
applies repository IDs in SQL before LIMIT/OFFSET, and the response adapter
retains a row-level access filter as defense-in-depth. Public coverage is
computed after that filter: adding denied rows, including rows carrying a pool
cap marker, cannot change the response.

Developer patch guidance remains unsafe when authorized evidence is missing,
truncated, or partial. No status, partial reason, or truncation field is emitted
solely because denied rows were removed.

## Verification

The regression suite covers all three change-planning routes at offsets 0 and 5.
It compares empty authorized results with hidden-only rows, and authorized-only
rows with the same rows plus denied rows whose `PoolTruncated` flag is both
false and true. A handler spy verifies that an empty scoped grant performs zero
content-store reads.

The focused regression and compatibility tests passed:

```text
go test ./internal/query -run '^(TestChangePlanningCoverageIgnoresDeniedTopicRows|TestChangePlanningEmptyScopedGrantSkipsContentRead|TestInvestigateChangeSurfaceMarksCandidatePoolTruncation|TestInvestigateChangeSurfaceMarksEmptyOffsetPoolStatusUnknown)$' -count=1
exit 0

go test ./internal/query/... ./internal/mcp/... ./internal/queryplan/... -count=1
exit 0
```

The final-byte PostgreSQL 18 disposable fixture passed:

```text
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgresql://eshu_test@127.0.0.1:55432/postgres?sslmode=disable' ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 go test ./internal/query -run '^TestInvestigateCodeTopicPathFirstPostgresLive$' -count=1 -v
exit 0
```

It applied all 168 migrations and proved the scoped repository predicate, the
capped 4,000-row topic pool, and empty offset-page coverage state. The fixture
used a temporary PostgreSQL 18 container; test cleanup dropped its generated
database and the container was stopped afterward.

Other passed gates: `scripts/verify-openapi.sh` (260 routes),
`scripts/verify-route-coverage.sh`, `scripts/verify-package-docs.sh`,
`scripts/verify-query-plan-regression.sh`, strict MkDocs build, and
`git diff --check`.

## Response-shaper performance

Benchmark Evidence: This is a CPU-only benchmark of the response adapter; no
Postgres, graph backend, endpoint, or queue work is included. Against base
`4af00ab98f9ad239e32b338025a1313d343209d0`, the candidate measured three shared
shapes on Go 1.27.1/darwin-arm64: 100 symbols, an empty page at offset 5,000,
and a capped topic pool. Ten alternating pairs used `-benchtime=500ms`,
`-benchmem`, and `GOMAXPROCS=1`; maximum sampled load1 was 4.95. The benchmark
inputs contained 100, 0, and 1 response symbols respectively. This synthetic
adapter benchmark has no SQL row set or terminal queue count.
The table below records the candidate after measurements, including the cost
of the added coverage metadata; it does not claim an endpoint latency gain.

The comparison is limited to the unchanged `changeSurfaceResponse` shaper and
three shared inputs: 100 symbols, an empty page at offset 5,000, and a capped
topic pool. Ten alternating base/candidate pairs used base
`4af00ab98f9ad239e32b338025a1313d343209d0` and candidate
`d0140a80bf6b285fc1960a7e81144d8748938a0b`, Go 1.27.1 on darwin/arm64, with
isolated worktree caches, `GOMAXPROCS=1`, `-benchtime=500ms`, and `-benchmem`.
The verified series had a maximum sampled load1 of 4.95; one over-bound base
pair was discarded and rerun. The handler shaper code and these three benchmark
inputs are unchanged by the later scoped-row correction. The old
`scoped_filter_unknown` sample is excluded because it represented the rejected
public status. Raw pair output and load samples are retained outside the
repository.

| Shape | Base median ns/op | Candidate median ns/op | Paired ratio mean ± SD | Base → candidate B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| 100 symbols, offset 0 | 14,527.5 | 14,871.0 | 1.0197 ± 0.0192 | 38,465 / 242 → 39,121 / 246 |
| Empty page, offset 5,000 | 1,637.5 | 1,973.5 | 1.2202 ± 0.0502 | 3,956 / 44 → 4,661 / 51 |
| Capped topic pool | 1,958.5 | 2,183.0 | 1.1097 ± 0.0203 | 4,760 / 51 → 5,417 / 55 |

These measurements show the cost of adding truthful coverage metadata to the
response shaper; they do not measure SQL or the handler endpoint. A later
same-harness rerun overlapped no named gate, but reached a sampled load1 of
19.71 with paired-ratio standard deviations of 8.3-12.1%; it is excluded as
unstable. The empty-grant correction short-circuits before the content-store
call, and its handler test verifies zero store reads. No endpoint latency
improvement or deployed p95 claim is made. The original seconds-scale planning
reads and under-one-second cold/warm p95 target remain open.

## Observability

Observability Evidence: The existing `query.*` handler spans and
`eshu_dp_api_request_duration_seconds` provide route-level duration evidence.
No endpoint p95 sweep was performed for this change.

No-Observability-Change: The change adds no metric, span, log field, queue
stage, or storage operation. The scoped-grant regression test verifies zero
content-store reads when the grant is empty; it does not add an operator signal.

No metric, span, log field, queue stage, or storage operation was added. Existing
`query.*` handler spans and `eshu_dp_api_request_duration_seconds` continue to
record route time.
