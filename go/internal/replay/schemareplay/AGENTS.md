# replay/schemareplay — agent scope

## Owned surface

- `go/internal/replay/schemareplay/` — the R-18 schema-version compatibility gate.
- `testdata/cassettes/replayschema/` — the FROZEN historical-version corpus.

## Non-negotiable invariants

- The gate MUST drive the production admission function
  `facts.ValidateSchemaVersion` (the projector's per-fact `AdmissionHook`). Do
  NOT re-implement the classification logic here — that would assert against a
  parallel gate and could pass while production silently projects wrong truth.
- Every frozen fact MUST have a pinned outcome that is either `admit` (nil error)
  or an explicit refusal whose error is asserted to contain a reason substring.
  A refused fact MUST never be allowed to look admitted (no silent-wrong).
- The corpus is FROZEN: do not regenerate it from the current registry. It
  represents historical recordings. When you intentionally change an outcome,
  change the pin in the test in the same commit and say why.
- Keep the registry pin guard (`TestSchemaVersionRegistryPinForcesCompatibilityCase`)
  intact and honest: a `fact_schema_version` bump MUST force either a frozen
  older-version admit case (proven migration) or an explicit refusal case. Do
  not loosen the guard to make a red bump pass.
- Synthetic/redacted payloads only — never commit real ARNs, account IDs, IPs,
  or hostnames.
- Stay credential-free: no Postgres, no graph backend, no Docker.

## Coverage TODO

Every core fact kind is currently at `1.0.0`, so the corpus cannot yet exercise
the backward-compat path (same MAJOR, older MINOR/PATCH admitted). The exact
`aws_resource@1.0.0` admit case becomes that proof the moment a kind advances
past `1.0.0`. When the registry first bumps a kind, add an explicit
older-same-major admit case (e.g. `aws_resource@1.0.x` against a bumped
`1.1.0`) so the `CompatibilitySupported` older-minor branch is covered, not just
the exact-match fast path.

## Skill routing

- `eshu-golden-corpus-rigor` for the frozen cassette + admission assertions.
- `golang-engineering` for Go edits and tests.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/schemareplay/ -count=1
```
