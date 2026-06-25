# Pre-commit Hooks

Local [pre-commit](https://pre-commit.com) hooks mirror the CI gates that most
often block PRs, so they fail fast at commit time instead of after a push. The
goal is simple: a PR that passes the hooks should not be bounced by these gates
on GitHub.

## Setup

```bash
pip install pre-commit            # or: brew install pre-commit
cd <repo root>
pre-commit install --install-hooks   # commit-stage hooks
pre-commit install --hook-type pre-push   # gosec runs at push time
```

The Go hooks bootstrap `golangci-lint` and `gosec` at the **exact versions CI
uses**, built with the repo's local `go` toolchain, on first run (cached under
`.git/eshu-precommit`). This is deliberate: do **not** rely on a brew/system
`golangci-lint` — the custom file-length plugin must be built with the same Go
build as the host binary, and a mismatched toolchain fails `plugin.Open`.

## What runs

| Hook | Stage | Mirrors CI gate |
| --- | --- | --- |
| trailing-whitespace, end-of-file-fixer, merge-conflict, check-yaml | commit | `git diff --check`, basic hygiene |
| check-added-large-files (≤1 MB) | commit | catches a stray committed Go build artifact |
| `go-fmt` (`golangci-lint fmt --diff`) | commit | the gofumpt formatting gate |
| `go-lint` (`golangci-lint run`) | commit | the `Lint Go` gate (errcheck, staticcheck, …) |
| `go-file-cap` | commit | the 500-line file cap (the `filelength` plugin) |
| `go-package-docs` | commit | `verify-package-docs.sh` (new packages need doc.go/README.md/AGENTS.md) |
| `capability-surface-inventory` | commit | the `surface_stale` drift gate (run the generator after catalog/command changes) |
| `go-gosec` | **push** | the gosec security gate (slow on Go 1.26, so push-time only) |
| `go-perf-evidence` | **push** | the hot-path performance-evidence gate (a change under storage/cypher, storage/postgres, collector, reducer, query, runtime, workers, or queues needs a tracked No-Regression / No-Observability marker in an `evidence-*.md` / README / AGENTS file). Diffs the branch against origin/main, so it runs at push time; needs bash ≥ 4. |
| `go-telemetry-coverage` | **push** | the telemetry-coverage gate (a new metric or pipeline stage must be reflected in `docs/public/observability/telemetry-coverage.md`, and the doc must not drift from `instruments.go`). Diffs the branch against origin/main. |

The lint/format/gosec hooks run only on the **changed packages**, so a normal
commit is fast. `golangci-lint` runs against a config copy with the custom
`filelength` plugin stripped (the cap is enforced by `go-file-cap` instead), so
the hooks never depend on the plugin's exact-toolchain build.

## Notes

- Bypass in an emergency with `git commit --no-verify` / `git push --no-verify`;
  CI is still the source of truth.
- Versions track CI automatically: the wrapper reads the `golangci-lint` and
  `gosec` pins from `.github/workflows/test.yml` and `security-scan.yml`.
- Implementation: `scripts/dev/precommit-go.sh` and `.pre-commit-config.yaml`.
