# First Five Minutes Benchmark

This benchmark proves that a new user reaches **one useful answer with bounded
evidence** within the first five minutes of onboarding. It is the measurable,
dogfoodable companion to `eshu first-run`: where `first-run` walks the smallest
truthful path from a checkout to one answer, this benchmark *scores* that run
against the onboarding success criteria and rejects any "answer" that is really
just a health or readiness signal.

For the guided run itself, see
[`eshu first-run`](../cli-reference.md). For the full verification map, see
[Local Testing](../local-testing.md).

## What It Measures

The benchmark consumes the canonical `{data, truth, error}` envelope that
`eshu first-run --json` already emits and scores these criteria:

| Criterion | Required | Source |
| --- | --- | --- |
| `first_answer_returned` | yes | `data.query_answered` + `data.query_summary` |
| `answer_has_truth_metadata` | yes | `truth.freshness`, `truth.completeness` |
| `answer_has_source_handles` | yes | `data.query_summary` / `data.repo_target` |
| `repository_indexed` | yes | `data.repo_indexed == "complete"` |
| `time_to_first_answer` | no | wall-clock measurement (or not-measured) |
| `manual_steps` | no | declared per-path constant |
| `failure_explanation_quality` | no | failing step detail + next steps |

Required criteria gate the verdict. Optional criteria record honest values: when
a metric cannot be derived in the current environment (for example wall-clock
time when scoring a saved artifact) the row is `not_measured` rather than a
fabricated number, and a `not_measured` optional row never fails the benchmark.

## The Health-Only Rejection Rule

This is the load-bearing correctness invariant of the benchmark:

> The benchmark FAILS when the "first answer" comes from health-only status
> rather than completed indexing and a returned bounded query.

Concretely, the verdict is **FAIL** when any of these hold, even if the runtime
reports healthy and readiness reports complete:

- `query_answered` is `false` (readiness/health alone is not an answer).
- `query_answered` is `true` but `query_summary` is empty (no evidence the
  query returned).
- The answer carries no truth metadata (`truth` is empty or missing
  freshness/completeness).
- The answer references no concrete source handle (a query that returned `0`
  repositories, or no repo target / example handle).
- `repo_indexed` is not `complete`.
- The envelope carries an `error`.

A run where the API is up and `/health` is green but no repository finished
indexing and no query returned is exactly the failure this benchmark exists to
catch. It must not be reported as a first-answer success.

## Running the Benchmark

The evaluator is a pure function exercised end to end through the
`eshu first-run-benchmark` subcommand. Capture an envelope, then score it:

```bash
# 1. Capture the first-run envelope for the path under test.
eshu first-run --json > /tmp/first-run.json

# 2. Score it. Non-zero exit means the benchmark FAILED.
eshu first-run-benchmark \
  --envelope /tmp/first-run.json \
  --path local_binary \
  --manual-steps 1
```

The command exits non-zero when the verdict is FAIL, so it can gate a dogfood
run in CI or a local acceptance script. Use `--json` to emit the machine
scorecard instead of the human table. Pass `--manual-steps` with the declared
copy/paste step count for the path; omit it (or pass a negative value) to record
the step count as not-measured.

`--path` is a free-form label for the onboarding path. Use `local_binary`,
`local_compose`, or `hosted` for consistent evidence.

## Path Coverage

| Path | How to capture | Status in this repo |
| --- | --- | --- |
| Local binary | `eshu first-run --json` against locally built binaries | Measured. A run with no reachable API correctly FAILS the benchmark (no answer returned). The PASS path is reproduced from a complete envelope below. |
| Local Compose | Bring up the Compose stack, then `eshu first-run --json` | Documented procedure. Run `docker compose up -d` (see [Docker Compose](../../run-locally/docker-compose.md)), wait for indexing, then capture and score the envelope. |
| Hosted | Point `--service-url` at a hosted API, then `eshu first-run --json` | Documented procedure. Capture the envelope against the hosted endpoint and score it. Redact the hostname before storing evidence. |

Only the local-binary path is fully measured in the default development
environment. Compose and hosted paths follow the identical capture-then-score
procedure; they are documented rather than measured here because they require a
running stack or a hosted endpoint that is not provisioned in this environment.

## Evidence (Redacted)

Store benchmark evidence in docs or issue comments **without secrets or private
hostnames**. Redact service URLs and repo paths to placeholders.

A complete, passing local envelope (redacted) and its scorecard:

```json
{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://REDACTED:8080",
    "repo_indexed": "complete",
    "repo_target": "/REDACTED/demo",
    "readiness": "indexing complete",
    "query_answered": true,
    "query_summary": "repositories query returned 1 (e.g. demo)"
  },
  "truth": {
    "level": "runtime",
    "freshness": "current",
    "completeness": "complete",
    "backend": "nornicdb"
  },
  "error": null
}
```

```text
First-answer benchmark PASSED
  path : local_binary
----------------------------------------
  [ok] * first_answer_returned: repositories query returned 1 (e.g. demo)
  [ok] * answer_has_truth_metadata: freshness=current completeness=complete
  [ok] * answer_has_source_handles: source handle: demo
  [ok] * repository_indexed: repository indexing completed
  [--]   time_to_first_answer: elapsed time not captured in this environment
  [ok]   manual_steps: declared manual copy/paste steps: 1
  [--]   failure_explanation_quality: run succeeded; no failure to explain
  (* = required; failure rejects the run)
```

A health-only envelope (readiness reports complete, but no bounded query
returned) is correctly **rejected**:

```text
First-answer benchmark FAILED
  path : local_binary
----------------------------------------
  [!!] * first_answer_returned: no bounded query returned (health/readiness alone is not an answer)
  [ok] * answer_has_truth_metadata: freshness=current completeness=complete
  [!!] * answer_has_source_handles: no answer returned, so no source handle is present
  [ok] * repository_indexed: repository indexing completed
  [--]   time_to_first_answer: elapsed time not captured in this environment
  [ok]   manual_steps: declared manual copy/paste steps: 1
  [!!]   failure_explanation_quality: failure did not explain the missing dependency or next steps
  (* = required; failure rejects the run)
```

## Verification

The evaluator is a pure, unit-tested Go function so the health-only-rejection
invariant is covered by tests, not only by manual runs:

```bash
cd go && go test ./cmd/eshu -run 'FirstRunBenchmark|EvaluateFirstAnswer|ParseFirstRunEnvelope' -count=1
```

The mandatory invariant test is `TestEvaluateFirstAnswerBenchmarkFailsOnHealthOnly`
(and its command-level counterpart `TestFirstRunBenchmarkCommandFailsOnHealthOnlyEnvelope`):
a result whose readiness/health looks complete but whose bounded query did not
return must produce a FAIL verdict and a non-zero exit.
