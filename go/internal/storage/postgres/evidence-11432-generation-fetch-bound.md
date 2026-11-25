# Evidence: bounded projector generation fetch (#11432)

`listFactsGeneration` used to issue one unbounded whole-generation `SELECT`
(including `payload`) with no `LIMIT` and no statement timeout. On QA that
read ran 32-92 minutes `DataFileRead`-bound against a 193 GB `fact_records`
(258 calls x 286 s mean in `pg_stat_statements`); the app-side 5-minute
admission-enqueue timeout then killed the worker and the retry re-read the
generation from zero.

The fetch is now keyset-chunked: pages of 500 ordered by
`(observed_at, fact_id)` inside one read-only repeatable-read transaction per
call, with a transaction-local 30 s statement timeout per page. A page that
dies partway fails the fetch through `projector.ErrGenerationFetchTimeout`
(SQLSTATE 57014 / deadline); a partial page is never returned as success.
There is no whole-generation deadline, and no new index: pages seek the
pre-existing `fact_records_scope_generation_keyset_idx` (migration 099).

No-Regression Evidence: theory shim ran BEFORE the change on a disposable
PG 18.6 with a forced generic plan and a 100k-fact generation fixture:
whole-generation fetch 22.6 ms EXPLAIN / 15,223 buffers / warm max 162.5 ms;
first page `LIMIT 500` 0.1 ms / 158 buffers / max 1.0 ms; cursor pages
(first/mid/tail) 0.1 ms / 158-160 buffers / max 1.1-1.4 ms; `LIMIT 2000`
variants 0.1-0.4 ms / up to 3.9 ms. Page size stays 500 and the timeout stays
30 s (>=2000x worst warm page, under the 60 s cap). Terminal proof:
5 new/extended fetch-bound tests green including `-race`, neighboring
`TestFactStore*`/`TestListFacts*`/`TestMassProbe*` green including `-race`,
full `postgres` package plus `projector/...` and `query/...` green except
`TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`, which fails
identically on the clean base (pre-existing environmental, needs
`ESHU_DEPLOY_CHECKOUT`); `go build ./...`, `go vet`, `gofmt`, and
`git diff --check` clean. Red-first honored: the extended tests failed before
the fix (old path issues no `set_config`).

No-Observability-Change: no new metric, span, log field, queue, graph write,
or runtime knob. Operators diagnose the path through the existing Postgres
query spans, the `load_facts` projection stage, and the typed
`failure_details` (`projector.ErrGenerationFetchTimeout`); a timed-out page
fails through the normal `Fail` path and stays retryable.
