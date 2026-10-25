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
changed paths. It is disabled as a required check for now; a red gate under it
is still a finding ([When a PR may merge](agent-git-hygiene.md#when-a-pr-may-merge)). The live Ifá/Odù cells (fault
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

## Go gates fail with a toolchain version mismatch

If a Go gate fails every package with
`compile: version "go1.X.Y" does not match go tool version "go1.X.Z"`, a
switched toolchain exported its `GOROOT` to a child that then ran the host `go`.
It happens when `go.mod` pins a newer `go` than the host binary and
`GOTOOLCHAIN=auto` re-execs the downloaded one. The `ci-gates` runner passes
that `GOROOT` to each gate command.

The repair belongs at the call site that runs `go` from the repo root: use
`env -u GOROOT go ...`, as `go_install_tool` in `scripts/dev/precommit-go.sh`
does. `scripts/test-precommit-go-toolchain-isolation.sh` guards it. For a gate
command you run by hand from the repo root, prefix it with `env -u GOROOT`.
Prefixing an outer command such as `make pre-push` does not clear what the
runner re-exports. Upgrading the host `go` to the `go.mod` version also removes
the cause.

A different message, `go: cannot find GOROOT directory`, means the shell exports
a `GOROOT` that no longer exists. Unset it.

A host `go` that is newer than `go/go.mod` causes the opposite failure.
`GOTOOLCHAIN=auto` never switches down, so a 1.27.x host builds a 1.26.10 module
with the newer compiler, and `golangci-lint` reports `typecheck` errors on
packages that are fine (`GOTOOLCHAIN=local` shows the same result). The floor
(`make pre-push`) therefore pins `GOTOOLCHAIN` to the `toolchain` or `go` line of
`go/go.mod` and unsets `GOROOT`; its lint, gosec and registry gates inherit both
(`scripts/lib/go-toolchain.sh`, tested by `scripts/test-go-toolchain.sh`). For a
command you run by hand, use `env -u GOROOT GOTOOLCHAIN=go1.26.10 ...` with the
version from `go/go.mod`. The first run with a toolchain that is not cached
downloads it, so it needs network access that once; offline and uncached, `go`
fails with "toolchain not available". CI is unaffected, because `setup-go`
installs the `go.mod` version. `scripts/lib/go-toolchain.sh` pins only a
`toolchain` line or a three-part `go` line; a language version such as
`go 1.22` is left alone.

## Hook test mirrors on macOS

`scripts/test-agent-hooks.sh` and `scripts/test-cursor-hooks.sh` block a second
live gate with a fake running gate: a process that `pgrep -x ci-gates` lists. A
script cannot be that process on macOS, because the system names a script
process after its interpreter. The fake is a small compiled Go program
(`scripts/lib/agent-hooks-fake-gate.go`) built into a temp dir as `ci-gates`.
`scripts/test-agent-hooks-fake-gate.sh` checks that `pgrep -x ci-gates` lists
the fake by its own pid, so a real gate running on the host cannot hide a broken
fake. A host with no Go toolchain prints a named `HOST-GAP` line and counts the
cases as skipped. Linux falls back to a shell fake and still runs them. Where Go
is installed, the same mirror asserts that the compiled fake is the one in use,
so CI cannot pass on the shell fake by accident, and that the shell fake returns
promptly when a caller captures it in a command substitution.

## The custom lint plugins

The bare `golangci-lint run ./...` above needs the repo's custom `filelength`
linter plugin built first in a fresh clone or worktree --
otherwise it fails with `plugin.Open` / "unable to load custom analyzer"
naming `tools/golangci-lint-filelength/filelength.so`. Build it once per
clone/worktree with `cd tools/golangci-lint-filelength && make build` (the
`.so` file is gitignored, so this is a per-checkout step, not a one-time repo
action). The directory cap is not a linter: it is `go/cmd/dirgate`.
`scripts/dev/precommit-go.sh lint`/`lint-all` (`lint` scoped to changed
packages is what `make pre-push` runs; `lint-all`, whole-module, is what
`make pre-pr` runs) avoid this entirely by running against a config copy with
both plugins stripped -- see that script's own header for why, and prefer it
over the bare command when you only need the day-to-day check, not a
`plugin.Open` diagnosis.
