# Agent Verification Details

This page holds the detail that the root [`AGENTS.md`](../../AGENTS.md) keeps
short under "Verification Defaults". The source of truth for gates stays
[Local Testing](../public/reference/local-testing.md).

## What `make pre-push` runs

After focused local proof and a preliminary full `eshu-code-review` with zero
P0/P1/P2-blocking findings, run `make pre-push` once, immediately before the
intended push or PR update. It is the fast local floor: changed-package
`go test` and `go test -race`, the file cap, changed-package gofumpt/lint/build/vet,
`go vet ./...` on the exact merge of HEAD with `origin/main` (a conflict fails
closed), the allowlisted fast registry gates for changed paths
(`local.pre_push: floor`; every other triggered gate prints `DEFER-CI` and still
runs in `make pre-pr` and CI), and the advisory docs-contradiction gate (its only enforcement). It has no whole-module race lane, no live Docker/NornicDB/Postgres
lane, and writes no stamp. `make pre-pr` and `make pre-pr-full` remain
available as deeper, RECOMMENDED (not required) preflights before pushing a
risky change: queue/lease/claim, schema DDL, hot-path Cypher or graph writes,
reducer projection/materialization, or a package move (prefer `pre-pr-full`
for moves — build tags can hide files from `./...`, so only its whole-module
race lane exercises them). Verify the preliminary review receipt against the
exact post-`pre-push` inputs before push. If verification fails, run a new
full `eshu-code-review`. CI stays authoritative. When `make pre-push` prints
`DEFER-CI` for a blocking gate on the surface you changed, run that gate
(`make pre-pr`, or the gate's `local.command` from the registry) before pushing
so CI is not its first run.

## Where the Ifá/Odù protection lives

The Ifá/Odù protection for contracts, performance, and end-to-end behavior
lives in CI, not in any local command: the `required-gates-complete` aggregate
(`.github/workflows/required-gates.yml`, alongside `go-core-complete` and
`go-race-complete`) collects every Ifá/Odù gate (fault injection, dead-letter
matrix, load saturation, replay drive, contract-layer, materialized-edge
coverage, determinism) plus every other `blocking: true` registry row for the
changed paths, and a merge requires it green. The live Ifá/Odù cells (fault
injection, determinism and dead-letter matrices) need Docker/NornicDB/Postgres
and never ran locally; `make pre-pr` ran only their static mirrors. The
hermetic Ifá rows (load saturation, contract-layer, materialized-edge coverage)
do run locally in `make pre-pr` and are deferred by `make pre-push`, so run
`make pre-pr` when a change touches `go/internal/ifa` or reducer
materialization.

## Common checks

```bash
cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/parser/... ./internal/collector/discovery ./internal/content/shape ./internal/collector -count=1
cd go && go test ./internal/terraformschema ./internal/relationships -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
cd go && golangci-lint run ./...
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Docs, root agent files, and README changes require the docs build plus
`git diff --check`.

## The custom lint plugins

The bare `golangci-lint run ./...` above needs the repo's custom `filelength`
and `dirgate` linter plugins built first in a fresh clone or worktree --
otherwise it fails with `plugin.Open` / "unable to load custom analyzer"
naming `tools/golangci-lint-filelength/filelength.so` or
`tools/golangci-lint-dirgate/dirgate.so`. Build both once per clone/worktree
with `cd tools/golangci-lint-filelength && make build` and
`cd tools/golangci-lint-dirgate && make build` (the `.so` files are
gitignored, so this is a per-checkout step, not a one-time repo action).
`scripts/dev/precommit-go.sh lint`/`lint-all` (`lint` scoped to changed
packages is what `make pre-push` runs; `lint-all`, whole-module, is what
`make pre-pr` runs) avoid this entirely by running against a config copy with
both plugins stripped -- see that script's own header for why, and prefer it
over the bare command when you only need the day-to-day check, not a
`plugin.Open` diagnosis.
