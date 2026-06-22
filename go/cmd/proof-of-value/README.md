# cmd/proof-of-value

## Purpose

`proof-of-value` is the operator entrypoint for Eshu's proof-of-value harness
(issue #3497). It produces real, reproducible evidence that an agent answers
IaC-reachability questions **more accurately with Eshu than with plain text
search ("grep")**.

There was previously no harness that demonstrated Eshu's answer-quality lift.
This command closes that gap with measured numbers, not claims.

## What it measures

Over the dead-IaC product-truth fixture corpus, for every curated artifact it
runs two real strategies against the same files on disk:

- **baseline_grep** — a faithful text-search agent. It calls an artifact `used`
  when its name appears elsewhere in the corpus and `unused` otherwise. It has
  no graph and no notion of a dynamic/ambiguous reference, exactly like an agent
  with only `grep`.
- **eshu** — the real `internal/iacreachability` analyzer.

Both are scored against the curated ground truth in
`tests/fixtures/product_truth/expected/dead_iac.json` by
`internal/proofofvalue`. The command prints per-strategy accuracy, dead-artifact
precision/recall, false positives/negatives, and the with-vs-without delta.

## Honesty boundary

Every number is computed from real tool output over the real corpus. The
command fabricates nothing: the Eshu answers come from the shipped analyzer, the
baseline answers come from literal text matching, and the ground truth is the
curated fixture truth file. If the corpus changes so that grep ties Eshu, the
delta shrinks honestly and the package test guard fails.

## Usage

```bash
cd go

# Print the scorecard.
go run ./cmd/proof-of-value

# Also write the JSON evidence artifact.
go run ./cmd/proof-of-value --out ../docs/public/reference/proofofvalue-evidence/dead-iac-v1.json
```

Flags:

- `--repo-root` — repository root holding `tests/fixtures` (auto-detected).
- `--corpus` — corpus label recorded in the report.
- `--out` — optional path for the JSON artifact.

## Interpreting results

- `accuracy` — fraction of questions answered with the correct label.
- `dead_prec` / `d_rec` — precision and recall of the `unused` (cleanup
  candidate) decision. Low recall means real dead artifacts were missed; a
  non-zero `dead_fp` means a live artifact was wrongly flagged for deletion.
- `delta` — `eshu - baseline`. Positive means Eshu did better.
- `dangerous mistakes avoided` — live artifacts grep would have flagged for
  deletion that Eshu did not.

## Files

| File | Responsibility |
| --- | --- |
| `main.go` | Flag parsing, orchestration, report printing, artifact write. |
| `corpus.go` | Load the fixture corpus and ground truth from disk. |

## Extending

To add another corpus (call-graph, correlation), add a `BuildRun`-style adapter
in `internal/proofofvalue` whose ground truth comes from a curated fixture truth
file, then wire a new mode here. Never derive ground truth from the tool being
measured.
