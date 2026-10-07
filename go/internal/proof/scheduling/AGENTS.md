# Proof-harness agent instructions

When changing fixture_seed.sql, oracle_preflight.sql, or persisted_oracle.sql,
run the README's exact psql checks on a disposable PostgreSQL 18 primary.
Require the green markers and all five seeded bad cases to exit 3; never run
the seed on ops-qa or a shared database.

Follow the repository root `AGENTS.md` and query, PostgreSQL, performance, and
review skills. This command is a non-deployed #7033 experiment; run its
focused tests before any remote proof, even though `go test ./...` now finds it.

Do not add a mode that can access a non-loopback database, write data, or run
without an explicit database/system-ID guard. Keep remote proof runs bounded,
read-only, corpus-fingerprinted, and followed by verified cleanup. A harness
timing result is not an endpoint or `<1s` acceptance result.
