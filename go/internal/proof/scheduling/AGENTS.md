# Proof-harness agent instructions

Follow the repository root `AGENTS.md` and query, PostgreSQL, performance, and
review skills. This command is a non-deployed #7033 experiment; run its
focused tests before any remote proof, even though `go test ./...` now finds it.

Do not add a mode that can access a non-loopback database, write data, or run
without an explicit database/system-ID guard. Keep remote proof runs bounded,
read-only, corpus-fingerprinted, and followed by verified cleanup. A harness
timing result is not an endpoint or `<1s` acceptance result.
