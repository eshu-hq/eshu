# AGENTS.md — internal/reducer/maintenance/config/artifacts

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Read first

1. `docs/internal/design/7126-ingest-time-config-artifacts-protocol.md`,
   sections 3 (derive protocol) and 4 (reader and walker).
2. `../../infra/` — the infra read model reconcile runner this walker
   follows.
3. `go/internal/storage/postgres/config/artifacts/` — the tables, the walk
   claim, and the dirty join.

## Import rule

This leaf must **never** import the parent `internal/reducer` package. It
also does not import storage: `cmd/reducer` adapts the storage functions to
`Claimer` and `Deriver`, as it does for the infra runner.

## What must stay conservative

- `Assign` must keep one repository on one worker for any page, and drop
  repeated ids. The hash is FNV-1a over `repo_id`; changing it moves work
  between workers, which is safe, but keep it deterministic.
- The claim is not a lease. Never make correctness depend on one walker
  owning a repository: the derive's advisory lock and advance-only write
  carry that.
- Serialization is not a fix: do not drop K to 1 to avoid a race.
- The backfill marker records only when a wrapped walk leaves the dirty join
  empty. Skipped repositories keep it pending; their next content Write or an
  extractor change retries them. Do not carve skipped repositories out of the
  marker condition without fresh arbitration: the record says every
  repository has a current state row.
- A read-model-off refusal (encoding_unsupported, fence_missing) aborts the
  cycle after counting the derives that ran. Never derive around it or retry
  it inside the cycle: nothing can succeed until the read model is installed.

## Gates that will fire on your change

- `verify-telemetry-coverage.sh` — a new `.go` file here needs a row in
  `docs/public/observability/telemetry-coverage.md`.
- `verify-package-docs.sh` — keep `doc.go`, `README.md` and this file in
  step with the exported surface.
