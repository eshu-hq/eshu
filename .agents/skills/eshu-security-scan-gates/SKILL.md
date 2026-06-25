---
name: eshu-security-scan-gates
description: |
  Debug and modify Eshu's security-scan workflow
  (.github/workflows/security-scan.yml): the Trivy filesystem/image, gosec,
  govulncheck, and nancy gates. ACTIVATE when editing security-scan.yml, bumping
  the Go toolchain (the `go` directive in go/go.mod) or a security dependency, or
  when any of those jobs (Trivy fs, Trivy image, gosec, govulncheck, nancy) is
  red (or red only in CI but green locally). Captures
  the Go-1.26 x/tools breakage, the trivy-action SARIF severity trap, the
  #nosec / gosec exit behavior, and — most important — the rule that you MUST
  reproduce with the exact CI tool version and flags, because a local
  approximation will lie to you.
---

# eshu-security-scan-gates

The `.github/workflows/security-scan.yml` jobs (Trivy fs, Trivy image,
govulncheck, gosec, nancy) fail in ways that do **not** reproduce with a naive
local run. This skill records the traps so the next Go bump or gate edit does
not cost another multi-hour diagnosis.

## Meta-rule: reproduce with the EXACT CI tool version + flags

Local-vs-CI drift was the single biggest time sink. Before concluding a gate is
clean:

- **Match the pinned tool version.** `aquasecurity/trivy-action@<v>` pins a
  specific trivy binary (e.g. v0.36.0 -> trivy 0.70.0; the run log prints
  "current version is 0.70.0"). A brew/`@latest` trivy can rate the same rule
  differently (its misconfig **checks bundle** is downloaded separately and
  versioned independently). Install the pinned version to reproduce.
- **Match the flags exactly**, including how they are passed (CLI flag vs the
  action's env var). A CLI `--severity` filter behaves differently from the
  action's `severity:` input — see the trivy trap below.
- **CI scans the PR merge commit** (`refs/pull/<n>/merge`), which folds in
  current `main`. Pull the exact findings from the uploaded SARIF:
  `gh api -H "Accept: application/sarif+json" repos/eshu-hq/eshu/code-scanning/analyses/<id>`
  and read each rule's `properties.security-severity` — the result `level`
  (note/warning/error) compresses the real score.
- A `-no-fail` local run does NOT prove the gate passes (it suppresses the exit
  code). Verify the gate the way CI does.

## Trivy: `format: sarif` ignores the `severity` filter by default

With `format: sarif`, `trivy-action` does NOT apply the `severity:` input to the
SARIF output or to the exit code — it emits every severity and `exit-code: 1`
fires on MEDIUM/LOW findings too (e.g. opinionated KSV/DS k8s/Docker hardening
rules on our own charts). This reproduces ONLY in CI; a local
`trivy fs --severity CRITICAL,HIGH` filters correctly and exits 0.

**Fix:** set `limit-severities-for-sarif: true` on every trivy step so the
declared threshold actually gates. Only CRITICAL/HIGH then block; MEDIUM/LOW
still upload to the Security tab for visibility.

**Genuine HIGH misconfigs** (e.g. KSV-0014 read-only root FS, KSV-0118 default
security context on a real manifest) must be FIXED, not suppressed — add a pod +
container `securityContext` (the eshu image runs as non-root uid 10001 and
writes only to its data volume, so `runAsNonRoot` + `readOnlyRootFilesystem`
with a `/tmp` emptyDir are safe).

**Intentionally-vulnerable fixtures and examples** belong in `skip-dirs`, not
"fixed": the govuln test corpus
(`go/internal/collector/vulnerabilityintelligence/testdata`) embeds known CVEs
to test the vuln collector, and the example collector-extension worker images
run as root by design (documented, to read the mounted docker socket).

## Go 1.26: gosec and govulncheck both break on x/tools SSA

Both build an SSA representation via `golang.org/x/tools`, which breaks on
Go 1.26 generics. As of mid-2026 even the latest released x/tools and x/vuln
master still hit it.

- **govulncheck** default symbol mode panics
  (`ForEachElement called on type containing *types.TypeParam`). Fix: run
  `govulncheck -scan package ./...` — package mode resolves vulnerable imports
  without the SSA call graph, does not panic, and is strictly more conservative
  than symbol mode (it flags an imported vulnerable package even when no
  vulnerable symbol is called), so it never hides a vulnerability.
- **gosec** v2.22.0 lets the SSA panic abort the job; bump to >= v2.27.1, which
  recovers ("skipping SSA analysis"). BUT v2.27.1 then logs "package
  query/reducer has type errors" (its bundled go/types cannot fully load some
  Go 1.26 generics) and **exits non-zero even with zero findings**. Fix: run
  `gosec -no-fail -fmt=sarif -out gosec.sarif ./...` and gate on the finding
  count instead of gosec's exit code:
  `jq '[.runs[].results[]] | length' gosec.sarif` — any reported result fails
  the job; the benign type-check artifact (which carries no SARIF results) does
  not. gosec on Go 1.26 is slow (SSA recover per package): emit SARIF once (not
  a second text pass) and set `timeout-minutes: >= 30`.
- **Stdlib CVEs:** `govulncheck -scan package` and trivy report Go-1.26.0 stdlib
  CVEs. The toolchain is the fix — bump the `go` directive in `go/go.mod` to a
  patched patch release (e.g. `go 1.26.4`); `setup-go` installs exactly what the
  `go` directive says via `go-version-file`.

## gosec false positives: it ignores `//nolint:gosec`

Standalone gosec does NOT honor golangci-lint's `//nolint:gosec`, so it re-flags
sites the repo already vetted. Suppress with a gosec-native
`// #nosec <RULE> -- <specific, truthful reason>` placed as the FIRST directive
in the comment (before any pre-existing `//nolint`); gosec only honors `#nosec`
when it leads. gosec/noctx are NOT in the golangci-lint enabled set
(`go/.golangci.yml`, `default: none`), so the trailing `//nolint` is inert for
the lint gate. Suppress per-site (never blanket-exclude a rule), and FIX real
issues (weak file/dir perms, missing `ReadHeaderTimeout`, math/rand for
security, unparameterized SQL) rather than annotating them.

## SQL/Cypher (`G201`/`G202`/`G703`) — suppress only with proof

Suppress only when the interpolated part is a compile-time constant or a fixed
internal identifier (and the values are bound parameters, e.g. `$N`
placeholders). If any user/request-derived value is concatenated into the query
text, that is a real injection bug — parameterize it.

## Concurrency block for `workflow_run`

For `workflow_run` events GitHub sets `github.ref` to the default branch, so a
naive `${{ github.workflow }}-${{ github.ref }}` concurrency group coalesces
every release image scan into one group and GitHub drops queued runs. Key the
group on the triggering run's head_sha:
`${{ github.workflow }}-${{ github.event_name }}-${{ github.event.workflow_run.head_sha || github.ref }}`.

## Touching hot-path files triggers the performance-evidence gate

The gosec `#nosec` triage (or any edit) under hot-path locations
(`storage/cypher`, `storage/postgres`, `collector`, `reducer`, `query`, …) makes
`scripts/verify-performance-evidence.sh` (the test.yml "Verify hot-path
evidence" step) require an evidence note. For a non-behavioral change, add a
`No-Regression Evidence:` + `No-Observability-Change:` marker to a recognized
evidence file (`docs/public/adrs/*.md`, `docs/public/reference/*.md`,
`go/**/README.md`, `go/**/AGENTS.md`, `go/**/evidence-*.md`) changed in the PR.
Note: on `main` this step fails earlier, which silently skips the "Lint Go"
step — so a PR that makes it past the hot-path step can be the first to surface
pre-existing golangci-lint debt (oversized files, unused funcs). Fix that debt
in the same PR.
