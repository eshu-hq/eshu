# semantic-eval-currentpath

## Purpose

`semantic-eval-currentpath` runs the Phase 0 semantic retrieval baseline against
Eshu's existing HTTP API. It reads a checked-in current-path eval suite, calls
bounded query endpoints through `go/internal/semanticeval/currentpath`, and
scores the observed ranked handles with `go/internal/semanticeval`.

## Ownership boundary

This binary owns only the one-shot CLI wrapper. It does not own the scoring
contract, HTTP request construction, API handlers, graph backend, Postgres
content store, or future NornicDB semantic retrieval path.

## Entry points

- `main` and `run` in `go/cmd/semantic-eval-currentpath/main.go`
- `eshu-semantic-eval-currentpath --version` and `-v` print the build-time
  version before reading suites or calling the API

## Flags

- `--suite` is required and points to a current-path suite JSON file.
- `--base-url` points to the Eshu API. Defaults to `ESHU_API_URL`, then
  `http://localhost:8080`.
- `--repo-id` replaces `{repo_id}` placeholders in suite scopes and expected
  handles, `must_not_include`, and current-path artifact exclusions. It is
  required when the suite contains placeholders. Use the canonical repository id
  from the Eshu repository catalog.
- `--run-output` writes the observed current-path run JSON.
- `--report-output` writes the scored report JSON. When omitted, the report is
  printed to stdout.
- `--k` controls the top-K cutoff. It defaults to 10.
- `--timeout` optionally overrides the per-case request timeout.

## Example

```bash
cd go
go run ./cmd/semantic-eval-currentpath \
  --suite ./internal/semanticeval/currentpath/testdata/eshu_phase0_suite.json \
  --repo-id repository:r_example \
  --base-url http://localhost:8080 \
  --run-output /tmp/eshu-currentpath-run.json \
  --report-output /tmp/eshu-currentpath-report.json
```

## Telemetry

The binary does not register metrics or tracing. It relies on the target Eshu
API's existing `/metrics`, traces, structured logs, response truth envelope, and
request latency captured in the eval run.

## Gotchas / invariants

- It is intentionally read-only against Eshu.
- It only calls the bounded modes accepted by the currentpath package.
- Checked-in suites may use `{repo_id}` so local canonical ids do not leak into
  repository docs.
- Current-path `exclude_handles` is only for harness artifacts such as the
  checked-in suite file. Real wrong answers should stay visible through
  `must_not_include`.
- A successful run does not prove semantic retrieval value by itself; it is the
  baseline that future NornicDB-backed runs must beat.

## Related docs

- [Semantic eval package](../../internal/semanticeval/README.md)
- [Current-path runner](../../internal/semanticeval/currentpath/README.md)
- [NornicDB semantic retrieval ADR](../../../docs/docs/adrs/2026-05-15-nornicdb-semantic-retrieval-evaluation.md)
