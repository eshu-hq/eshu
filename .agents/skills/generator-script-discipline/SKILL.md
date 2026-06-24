---
name: generator-script-discipline
description: |
  Use when a change adds a shell script that produces a generated artifact
  and commits it to the repo: a JSON schema, a Grafana dashboard, a
  verifier-generated file, a manifest, a fixture catalogue, a typed-API
  definition, or any other file whose committed form is a function of
  source-of-truth data plus a deterministic transformation. Activate for
  new scripts under `scripts/` that emit files under `docs/`,
  `go/internal/*/data/`, `deploy/`, or any checked-in artifact directory.
  Captures the three-file pattern (slim `generate-*.sh` + `lib/` chunks +
  `test-generate-*.sh` mirror), idempotency as a first-class test case,
  and the 500-line cap pre-planned rather than retrofitted.
---

# generator-script-discipline

Use this skill whenever a change adds a shell script that produces a
generated artifact and commits it to the repo: a JSON schema, a
Grafana dashboard, a Verifier-generated file, a manifest, a fixture
catalogue, a typed-API definition, or any other file whose committed
form is a function of source-of-truth data plus a deterministic
transformation. Activate for new scripts under `scripts/` that emit
files under `docs/`, `go/internal/*/data/`, `deploy/`, or any
checked-in artifact directory.

This is the discipline Epic X4 (operator dashboard) and S2/S3
(skillgen) converged on. Both shipped a generator + test mirror + CI
gate, both required idempotency, both needed a `lib/` split to keep
the script under the 500-line cap, and both had to be wired into GHA
the same way.

## When To Use

- Adding a new `scripts/generate-*.sh` (or `*.py`, `*.ts`, etc.) that
  writes a checked-in artifact.
- Refactoring an existing generator to make it idempotent or to add
  its own test mirror.
- Adding a CI workflow that enforces a generator's output is in sync
  with what the generator produces.
- Adding or editing a `scripts/lib/*.sh` that holds data registries
  (metric names, panel definitions, etc.) sourced by a generator.

## The Three-File Pattern

Every generator in this repo should land as three files:

1. **`scripts/generate-<name>.sh`** — the entry point. Sources the lib,
   invokes a function (or runs a `cat <<EOF` heredoc), writes to the
   output path. Should be the *slimmest* of the three files; under
   100 lines if possible.
2. **`scripts/lib/<name>-*.sh`** — the data registry and the panel /
   fragment / template definitions. Multiple files if the data is
   large. Each file under the 500-line cap.
3. **`scripts/test-generate-<name>.sh`** — the test mirror. Asserts
   idempotency, asserts the output is well-formed, asserts the
   headline content is present. Must run without a live runtime
   dependency (no Postgres, NornicDB, or Go build).

Plus, when the generator's drift is a release-blocker (the operator
dashboard drift would silently produce a wrong panel):

4. **`.github/workflows/generate-<name>.yml`** — the CI gate. Runs the
   test mirror in one job, runs the generator and asserts
   `git diff --exit-code` on the output in another.

## Idempotency Is A First-Class Test

A generator that produces different bytes on a clean re-run is a bug,
not a feature. The test mirror MUST include an idempotency case as
its first check:

```bash
# Case 1: generator is idempotent — re-running with the same inputs
# produces the same bytes. (Deterministic output is the load-bearing
# property of the gate.) Use the byte-comparison form below, which is
# portable across macOS and the GHA ubuntu-latest runner.
if cmp -s "${output_path}" "${output_path}.bak"; then
  record_pass "generator is idempotent on a clean re-run"
else
  record_fail "generator output is not byte-for-byte deterministic"
fi
```

The byte-comparison form matches the convention used by
`scripts/test-verify-telemetry-coverage.sh` and
`scripts/test-generate-operator-dashboard.sh`. Capture the expected
output once with `cp "${output_path}" "${output_path}.bak"` before
running the second pass; if the second pass produces the same bytes,
the generator is deterministic. Do not use `md5 -q` (macOS-only) or
`md5sum` (Linux-only) — `cmp -s` works on both and is the repo
convention.

This catches timestamp embedding, hostname leaks, unkeyed `map`
iteration in templating languages, and any other non-determinism that
would otherwise only show up when CI runs.

## The 500-Line Cap Is Real, Plan The Split Up Front

The repo's `AGENTS.md` requires every file under 500 lines. A
generator that emits a Grafana dashboard with 20 panels will exceed
500 lines on the first draft. **Plan the lib/ split before writing
the heredoc.** The split has two natural axes:

- **Data** vs **structure**: a `lib/<name>-metrics.sh` (or
  `lib/<name>-fragments.yaml`) holds the data; the main script holds
  the structure that consumes it.
- **By row / by section**: a Grafana dashboard with 5 rows splits as
  `lib/<name>-panels-{1,2}.sh`, where each file emits the panels for
  one or two rows. The main script concatenates with `,` between
  functions.

The split should be visible in the directory listing before the
generator reaches 200 lines, not after it crosses 500.

## Test Cases That Catch Real Bugs

The 8/8 cases in `scripts/test-verify-telemetry-coverage.sh` and the
7/7 cases in `scripts/test-generate-operator-dashboard.sh` are the
worked examples. The patterns that caught real bugs:

- **Idempotency** (above).
- **Top-level shape**: parse the JSON / YAML with `jq` or `yq` and
  assert `title`, `uid`, `schemaVersion`, or schema-required keys
  are present.
- **Cross-link enforcement**: for every data name in the registry,
  assert the generated output references it. This is the link
  between "the source of truth changed" and "the artifact kept up".
  For the dashboard, every `eshu_dp_*` in the metrics lib must
  appear in some panel expression.
- **Negative cases**: at least one negative case that proves the
  script can fail. The "doc references unregistered metric" case in
  the X2 test mirror is the model.

## CI Workflow Shape

Mirror the existing `verify-skill-roundtrip.yml` and
`verify-telemetry-coverage.yml` workflows:

```yaml
name: Generate <Name>

on:
  pull_request:
  push:
    branches:
      - main

permissions:
  contents: read

jobs:
  test-generate:
    name: Verify <name> test mirror
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with: { go-version-file: go/go.mod }
      - run: sudo apt-get update && sudo apt-get install -y ripgrep jq
      - run: scripts/test-generate-<name>.sh

  generate:
    name: Verify <name> gate
    runs-on: ubuntu-latest
    needs: test-generate
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with: { go-version-file: go/go.mod }
      - run: sudo apt-get update && sudo apt-get install -y ripgrep jq
      - name: Generate
        shell: bash
        run: |
          set -o pipefail
          scripts/generate-<name>.sh 2>&1 | tee /tmp/<name>.log
      - name: Check drift
        run: |
          if ! git diff --exit-code -- <output-path>; then
            { echo "re-run: scripts/generate-<name>.sh"; git diff; } >&2
            exit 1
          fi
      - name: Upload drift report on failure
        if: failure()
        uses: actions/upload-artifact@v4
        with: { name: <name>-drift-report, path: /tmp/<name>.log, if-no-files-found: warn }
```

Two jobs: `test-generate` (mirror) and `generate` (gate). The `generate`
job re-runs the generator and uses `git diff --exit-code` to assert the
output is in sync. On failure, the drift log is uploaded as an artifact
so the reviewer can see what changed.

## Failure Modes

| Failure | What to do |
| --- | --- |
| `jq` not installed | Add `sudo apt-get install -y jq` to the workflow step. `jq` is the standard for shell-side JSON validation in this repo. |
| `mkdocs build --strict` rejects a link to a generated JSON | The JSON is not a documentation page. Reference it by prose, not by markdown link. The X4 dashboard is the worked example. |
| The 500-line cap blocks the script | Pre-plan the lib/ split. The dashboard generator split 3 ways: data (`lib/operator-dashboard-metrics.sh`) + 2 panel chunks (`lib/operator-dashboard-panels-{1,2}.sh`). |
| Test mirror passes locally but fails in CI | Probably a `PATH` or `LANG` issue. Use `LC_ALL=C` and absolute paths in the test mirror. |
| Generator produces different bytes on every run | Idempotency violation. The likely culprits are timestamps, unkeyed map iteration, or `sort | uniq` without a stable order. Add a stable sort, remove the timestamp, or move the generator to a language that sorts deterministically. |
| The committed artifact is out of date with the generator | The PR author regenerated locally but forgot to commit. The CI gate catches this; the fix is to run the generator and commit the result. |
