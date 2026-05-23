# cmd/reducer Agent Rules

These rules apply only inside `go/cmd/reducer/`. Root `AGENTS.md` still
controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/reducer/README.md`
- `go/cmd/reducer/doc.go`
- `go/cmd/reducer/main.go`
- `go/cmd/reducer/config.go`
- `go/internal/reducer/README.md`
- `go/internal/reducer/AGENTS.md`
- `docs/public/reference/nornicdb-tuning.md`

## Local Invariants

- MUST fail startup for invalid `ESHU_GRAPH_BACKEND` values.
- MUST keep reducer domain behavior in `internal/reducer`; this command only
  wires process config, adapters, runners, and admin hosting.
- MUST keep graph backend differences in command wiring or storage/Cypher
  seams, not in reducer domain handlers.
- MUST keep `ESHU_REDUCER_CLAIM_DOMAIN` and
  `ESHU_REDUCER_CLAIM_DOMAINS` mutually exclusive.
- MUST keep the local-authoritative NornicDB projector-drain gate scoped to the
  intended profile/backend gate.
- MUST keep reducer heartbeat timing tied to the work-queue lease duration.
- MUST keep `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` conformance-only unless a
  tracked benchmark and observability note promote it.
- MUST keep `GraphProjectionPhaseRepairer` and graph-drain wiring in place;
  they are recovery and scheduling paths, not alternate truth sources.

## Change Gates

- New env vars MUST be parsed in `config.go`, wired in `buildReducerService`,
  documented in the command README and runtime docs, and covered by config
  tests.
- Worker, claim-limit, retry-delay, or batch-size changes MUST include queue,
  conflict-key, graph-write, and no-regression evidence.
- New runners MUST define domain behavior in `internal/reducer`, then wire the
  runner here with focused service tests.
- Drift prior-config-depth changes MUST update the Postgres default, reducer
  config tests, and the command README.

## Focused Verification

```bash
cd go
go test ./cmd/reducer -run 'Test.*Config|Test.*Wiring|Test.*Claim|Test.*Backend|Test.*WorkloadDependency' -count=1
go test ./cmd/reducer ./internal/reducer -count=1
```
