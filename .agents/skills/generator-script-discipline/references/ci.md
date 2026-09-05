# Generator CI integration

Paths in backticks are repository-relative.

## CI Workflow Shape

Older generators (skillgen roundtrip, telemetry coverage) each shipped as
their own single-purpose workflow file with this two-job shape. Since the
#4218 consolidation, new gates are added as a matrix entry inside
`.github/workflows/static-contract-gates.yml` instead of a new workflow
file — see the `skill` and `telemetry` entries there (the "Verify
skillgen roundtrip gate" and "Verify telemetry coverage gate" matrix jobs)
for the current pattern: one `changes` job filters paths with
`dorny/paths-filter`, and one shared matrix job runs each selected gate's
`test` command then its `gate` command, reusing one checkout/Go/ripgrep
setup instead of duplicating it per generator.

A standalone workflow is still the right shape when a generator's drift
check needs its own trigger, permissions, or artifact upload that does not
fit the shared matrix — for example, if the drift log itself must be
uploaded on failure:

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
so the reviewer can see what changed. Prefer adding a matrix entry to
`.github/workflows/static-contract-gates.yml` over this standalone shape
unless the artifact-upload or bespoke-trigger need above applies.
