# Graph capture

`capture` persists and diffs the differential statement recordings for issue
#6782: every production Cypher statement a golden-corpus replay executes
must return the same rows on NornicDB and Neo4j.

## Where this fits

```mermaid
flowchart LR
  S["replay tiers\n(bootstrap-index, projector,\nreducer, api, mcp-server)"] --> W["capture.Session\n(WrapExecutor / WrapGraphQuery)"]
  W --> J["JSONL recordings\n(ESHU_DIFFERENTIAL_CAPTURE_DIR)"]
  J --> D["capture.Compare\n+ divergence allowlist"]
  D --> G["backend-differential\ngate job (non-blocking)"]
```

Capture is env-gated (`ESHU_DIFFERENTIAL_CAPTURE=1` plus
`ESHU_DIFFERENTIAL_CAPTURE_DIR`): without the flag every decorator is a
passthrough, so normal runs pay one env read at startup and nothing per
statement. A set flag without a directory fails closed at startup instead
of running a replay that records nothing.

## Internal structure

```text
capture/
  doc.go                  — package contract
  sink.go                 — JSONL sink plus directory loader, grouped by backend
  allowlist.go            — divergence allowlist parse, excuse, and validation
  diff.go                 — Compare: diff two recording directories with a
                            bounded report
  decorate.go             — Session: env-gated recorder plus sink binding for
                            the read and write seams
  differential_live_test.go — live capture-to-diff proof on both backends
                            (skips without ESHU_DIFFERENTIAL_LIVE=1)
```

## Ownership boundary

`capture` owns recording persistence, the diff driver, and the allowlist
contract. It does not own statement fingerprinting, row digests, comparison
kinds, or the seam decorators — those live in `backendconformance`
(`differential.go`, `differential_kinds.go`)
— and it does not own the replay, the gate phases, or the CI job, which
live in `cmd/golden-corpus-gate`, `scripts/verify-golden-corpus-gate.sh`,
and `.github/workflows/golden-corpus-gate.yml`.

## Exported surface

- `Session` — `Open(getenv, binary)` starts a session (nil when off);
  `Reader`/`Writer` decorate the seams; `Close` flushes to the sink.
- `Compare(left, right string, allow *Allowlist, w io.Writer) error` —
  diffs two recording directories, excusing allowlisted divergences.
- `ParseAllowlist` — parses `specs/backend-divergence-allowlist.v1.yaml`;
  missing reason, missing/unknown tier, missing/non-issue upstream, and
  stale entries fail. The tier scopes each entry to one divergence kind
  (`missing`, `results`, `executions`, `failures`, `rowcount`) or to the
  whole statement; executions-tier entries are exempt from staleness
  because agreeing counts match nothing on some runs.
- `OpenDir` / `LoadDir` — JSONL sink and loader; unknown backends fail on
  both sides.

## Telemetry

None. Capture adds no metrics, spans, or logs of its own: recording must
be observable-neutral so the replay under test behaves exactly as it does
without capture. Gate outcomes surface through the existing B-7 gate
reporting, not through this package.
