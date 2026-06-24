# MIT SPDX Header on Every Go File Design

Status: implemented and merged (PRs #3842, #3844). No open design decisions.

Issue: #3734 (Epic L). L1 (#3766): metadata verification. L2 (#3767):
generator + verifier + CI gate + per-file application.

Owners: CI/ops + governance:legal.

## 1. Problem

The repo shipped an MIT LICENSE file at root (`Copyright (c) 2025-2026
eshu-hq`) with a README badge, but zero of ~8,700 `.go` files carried a
per-file SPDX assertion. Tooling that scans at the file level (SBOM
generators, license compliance scanners, downstream dependency auditors)
cannot infer the project license from a file that lacks an inline SPDX
identifier.

## 2. Design

Per-file header (lines 1-2, then a blank line per Go convention):

```
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
```

The SPDX identifier matches the root LICENSE. No relicensing — the project
stays MIT.

### Generator (`scripts/add-license-header.sh`)

- Idempotent: re-running on an already-headered tree produces byte-identical
  output.
- Refuses to run in the main checkout: requires `.git` to be a regular file
  (git worktree), per AGENTS.md. The `ESHU_LICENSE_HEADER_REPO_ROOT` env var
  overrides for test mirrors.
- Strips existing non-canonical license blocks before writing. If a file
  carries a stale `SPDX-License-Identifier: Apache-2.0` or a lone `Copyright`
  line, those are removed before the canonical MIT header is inserted. This
  prevents a conflicting SPDX declaration from surviving below the new header.
- Preserves `//go:build` and `// +build` constraints: the header is placed
  above the constraint with a blank-line separator.
- Discovery uses `rg --files -g '*.go'` per AGENTS.md (never `find`).

### Verifier (`scripts/verify-license-header.sh`)

- Fails the build (exit 1) if any `.go` file is missing lines 1-2.
- Uses bash builtins (`read`) — no per-file fork. Full-repo pass on ~8,700
  files completes in under 2 seconds in CI.
- Discovery uses `rg --files` per AGENTS.md.

### Test mirror (`scripts/test-verify-license-header.sh`)

Thirteen scenarios: presence check, missing/wrong SPDX, wrong copyright,
build constraint, empty tree, generator idempotency on fresh/pre-headered
trees, stale SPDX block replacement, Copyright-only replacement, build
constraint preservation, and `rg --files` discovery.

### CI gate

Hooked into `.github/workflows/test.yml` as a pre-merge step immediately
after `verify-package-docs.sh`:

```yaml
- name: Verify Go license headers
  run: |
    scripts/test-verify-license-header.sh
    scripts/verify-license-header.sh
```

### Comment-only change detection

The existing `verify-performance-evidence.sh` gate previously flagged every
`.go` file under hot-path directories (`go/internal/storage/postgres/`,
`go/internal/reducer/`, etc.) as requiring performance evidence — including
the batch SPDX-header rollout of ~5,200 such files.

A new `is_comment_only_change` helper was added to that script. It pre-fetches
the full diff once, builds a per-file `0=comment-only / 1=code-change` map,
and skips files whose only changes are Go comments (`//`, `/*`, `*`, `*/`),
shell/YAML comments (`#`), or blank lines. Real code edits in hot-path files
still trip the gate.

### Collector entrypoints generator

`go/internal/collector/entrypoints/generate.go` was extended so the
`managedHeader` constant emits the SPDX header before the "managed by
manifest" comment. This keeps generated `config.go`, `main.go`, and
`service.go` files byte-identical between the SPDX rollout and future
regeneration runs.

### Chart metadata

`deploy/helm/eshu/Chart.yaml` declares the chart license through the
canonical Artifact Hub annotation:

```yaml
annotations:
  artifacthub.io/license: MIT
```

This places the metadata where Helm 3.3.2+ (which disallows extra top-level
`Chart.yaml` fields) and Artifact Hub expect it.

## 3. Files Not Covered

- `package.json` and `apps/console/package.json` are `"private": true`
  packages. npm convention omits the `license` field on private packages.
- `go/cmd/eshu/component_init.go` is a CLI scaffolder that writes new
  collector packages to user-specified output directories (not checked in).
  Its templates will receive SPDX headers in a separate UX pass.

## 4. Verification Evidence

- `scripts/test-verify-license-header.sh`: 13/13 test scenarios pass.
- `scripts/verify-license-header.sh`: all ~8,700 `.go` files carry the header.
- `scripts/add-license-header.sh` rerun: 0 added (idempotent).
- `scripts/verify-performance-evidence.sh` on the rollout tree: exit 0.
- `scripts/verify-collector-entrypoints-generated.sh`: current.
- `golangci-lint run ./...`: 0 issues on header lines (comments exempt from
  line-length and formatting rules).
- `go test ./internal/collector/entrypoints/... -count=1`: passes.
- `gofmt`: clean on spot-checked files.
- `go build ./cmd/eshu ./cmd/api ./cmd/ingester ./cmd/reducer ./cmd/bootstrap-index`:
  clean.
- `git diff --check`: clean.

## 5. References

- Root LICENSE: MIT, Copyright (c) 2025-2026 eshu-hq.
- `docs/public/license.md`: mirrors the LICENSE text.
- `scripts/add-license-header.sh`
- `scripts/verify-license-header.sh`
- `scripts/test-verify-license-header.sh`
- `.github/workflows/test.yml`
