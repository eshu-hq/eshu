# Proof-of-Value Harness

The proof-of-value harness answers one question with measured numbers: **does an
agent answer better with Eshu than without it?** It was added for issue #3497,
which noted that Eshu had no harness demonstrating end-to-end answer-quality
lift.

## What it compares

The harness runs a representative IaC-reachability task set over a known fixture
corpus and scores two strategies against curated ground truth:

- **baseline_grep** — a faithful "agent with only text search" strategy. It
  calls an artifact `used` when its name appears elsewhere in the corpus and
  `unused` otherwise. It has no graph and no notion of a dynamic/ambiguous
  reference, exactly like an agent restricted to `grep`.
- **eshu** — the real `internal/iacreachability` analyzer over the same files.

Both verdicts are computed at run time from real tool output. Ground truth comes
from the curated fixture truth file, never from the tool under measurement, so
the reported delta cannot be fabricated.

## Corpus and ground truth

- Corpus: `tests/fixtures/product_truth/dead_iac` — a synthetic, public-safe
  multi-repository corpus covering Terraform, Helm, Kustomize, Ansible, and
  Docker Compose, with used, unused, and dynamic (ambiguous) artifacts.
- Ground truth: `tests/fixtures/product_truth/expected/dead_iac.json` — 18
  per-artifact assertions of expected reachability.

## How to run

```bash
cd go
go run ./cmd/proof-of-value
go run ./cmd/proof-of-value --out ../docs/public/reference/proofofvalue-evidence/issue-3497-dead-iac-v1.json
go test ./internal/proofofvalue ./cmd/proof-of-value -count=1
```

## How to read the scorecard

- `accuracy` — fraction of questions answered with the correct label.
- `dead_prec` / `d_rec` — precision and recall of the `unused` (cleanup
  candidate) decision. Low recall means real dead artifacts were missed; a
  non-zero `dead_fp` means a live artifact was wrongly flagged for deletion.
- `delta` — `eshu - baseline`; positive means Eshu did better.
- `dangerous mistakes avoided` — live artifacts grep would flag for deletion
  that Eshu does not.

## Measured result

Run over the 18-artifact dead-IaC corpus
(`docs/public/reference/proofofvalue-evidence/issue-3497-dead-iac-v1.json`):

| Strategy | Accuracy | Correct | Dead recall | Dead false positives |
| --- | --- | --- | --- | --- |
| baseline_grep | 0.556 | 10/18 | 0.400 | 0 |
| eshu | 1.000 | 18/18 | 1.000 | 0 |

Delta (eshu − baseline): **+0.444 accuracy**, **+0.600 dead recall**.

The eight baseline failures are explainable, not stacked: grep mislabels all
five dynamic-reference artifacts (`dynamic-target`, `dynamic_role`) as `used`
because the literal name appears inside a templated or interpolated path, and it
misses three unused artifacts. The two `orphan-cache` artifacts (a Terraform
module and a Compose service that share the name) each appear in the other's
files, so grep sees the token elsewhere and wrongly calls them `used` — a real
cross-artifact name collision a text search cannot disambiguate. `orphan_maintenance`
is named in an unreached playbook. Grep still gets the easy cases right —
including two artifacts (`orphan-worker`, `orphan-api`) it correctly calls
unused — so the baseline is faithful, not a strawman.

Note: the baseline excludes each artifact's own definition from its reference
search. For Compose services, whose analyzer artifact path is synthetic, the
declaring `compose.yaml` is excluded explicitly so a service's own YAML key is
never counted as a self-reference (regression-tested in
`internal/proofofvalue`). The remaining `orphan-cache` "used" verdict is the
genuine Terraform/Compose name collision above, not a self-reference.

## Honesty boundary

The harness reports misses, not just hits. The scorer has no bias toward Eshu;
if Eshu answered worse, the delta would go negative (covered by
`internal/proofofvalue` tests). The package test
`TestHarnessProvesEshuOutperformsBaselineOnRealCorpus` runs the full comparison
over the real corpus in CI and fails if the delta is not positive.

## Performance and observability

The harness is an offline scoring and aggregation tool. It runs over a small
static fixture corpus in `go test` / CI and touches no runtime hot path: there
is no Cypher, no graph write, no queue, no worker, no lease, and no batching in
`internal/proofofvalue` or `cmd/proof-of-value`. The Eshu strategy reuses the
existing `internal/iacreachability.Analyze` pure function unchanged.

No-Regression Evidence: not a runtime path; the package is pure CPU over an
18-artifact in-memory corpus. `go test ./internal/proofofvalue ./cmd/proof-of-value -count=1`
completes in well under a second.

No-Observability-Change: no runtime stage, queue consumer, or graph/Postgres
query is added or modified; the tool emits a stdout scorecard and an optional
JSON artifact only.

## Extending

The current harness covers IaC reachability. To extend it to call-graph or
correlation answer quality, add a `BuildRun`-style adapter in
`internal/proofofvalue` whose ground truth comes from a curated fixture truth
file (for example `tests/fixtures/product_truth/expected/graph_analysis.json`),
and wire a new mode into `cmd/proof-of-value`.
