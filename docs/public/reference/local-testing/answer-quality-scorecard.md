# Answer Quality Scorecard

The answer-quality scorecard measures whether Eshu answers are useful, truthful,
cited, bounded, comparable across surfaces, and safe to publish. It is the
dogfood gate for answer-surface changes after the first successful run: capture
real API, MCP, CLI, or hosted answers, redact the evidence, then score the
artifact with `eshu answer-quality-scorecard`.

For the first-answer onboarding gate, use
[First Five Minutes Benchmark](first-five-minutes-benchmark.md). For the answer
shape, use [Reading Eshu Answers](../reading-answers.md).

## What It Scores

Each evidence file must use `answer-quality-scorecard/v1` and include at least
one prompt from every representative family:

| Family | Expected surface coverage | Required proof |
| --- | --- | --- |
| `service_story` | API and MCP | Service story plus citation packet follow-up. |
| `code_topic` | API and MCP | Code-topic investigation plus relationship-story follow-up. |
| `incident_context` | API and MCP | Incident context with cited operational evidence. |
| `supply_chain_impact` | API and CLI | Finding impact with source or finding handles. |
| `documentation_truth` | API and MCP | Documentation evidence plus freshness check. |
| `freshness_readiness` | API and MCP | Readiness/freshness explanation, not health-only status. |
| `hosted_onboarding_governance` | Hosted and CLI | Hosted onboarding artifact plus shared-token caveat. |

The scorer records pass/fail rows for:

| Criterion | Pass condition |
| --- | --- |
| `usefulness` | The answer is supported, specific, and not generic, too verbose, or a circular identity-only restatement of the question's entity. |
| `truth_honesty` | Truth class and freshness are present and not over-confident. |
| `citation_coverage` | Every captured result has concrete evidence handles. |
| `boundedness` | Partial, truncated, stale, or unsupported answers say why and give a bounded continuation. |
| `narration_fallback` | Optional narrated rows preserve the deterministic fallback row and accepted narration passes the governed narration validator. |
| `parity` | Required API/MCP/CLI/hosted surfaces are present and agree on truth class. |
| `follow_up_usefulness` | Required next calls are present, especially for partial or truncated answers. |
| `publish_safety` | Evidence contains no private paths, hostnames, credentials, raw addresses, or sensitive excerpts. |

`publish_safety` covers a connection string carrying a password in its userinfo
on any scheme or none (`bolt://`, `postgres://`, `redis://`, and the bare
`svc:PW@host/tool` form), IPv4 and IPv6 raw addresses, and a password assignment
written with either `=` or `:` — including the underscore-joined env keys a
config dump actually carries, such as `DB_PASSWORD:` and `POSTGRES_PASSWORD:`.

The password rule reads the assigned value, not just the key, so an answer that
describes a schema (`password: string`) or counts resources
(`random_password: 3 resources`) is not withheld. The screen is best-effort by
design and `go/internal/answerguardrail/README.md` lists what it deliberately
misses; treat a passing scorecard as screened, not as certified.

A `publish_safety` failure names the field it found the value in — for example
`unsafe publishable evidence in api result answer_summary`, or `unsafe run
metadata in run_id` — and never prints the value. It cannot: the detail is
echoed to stdout, returned in the command's error, serialized into `--json`, and
copied into the suggested follow-up issue body, so printing it there would
publish exactly what the criterion refused. For the same reason, a `run_id` that
itself fails the scan is replaced in the verdict by
`[redacted: run_id failed publish safety]`. Search your own evidence file at the
named field to find the value.

## Capture And Score

The scorer is offline by design. It does not call API or MCP endpoints itself;
that keeps private service details out of the repository and makes the artifact
rerunnable in CI or during review.

```bash
# 1. Capture real answers from the surfaces under test.
# 2. Redact paths, hostnames, credentials, addresses, and sensitive excerpts.
# 3. Score the redacted artifact.
eshu answer-quality-scorecard --from /tmp/answer-quality-redacted.json
```

Use `--json` when comparing two runs programmatically:

```bash
eshu answer-quality-scorecard \
  --from /tmp/answer-quality-redacted.json \
  --json > /tmp/answer-quality-verdict.json
```

The command exits non-zero when any required criterion fails. Failure output
includes suggested follow-up issue titles and labels such as
`capability:answer-experience`, `answer:dogfood`, `answer:citations`,
`answer:parity`, or `capability:hosted-ops`.

## Evidence Shape

Use placeholders and redacted handles. Do not store raw source excerpts,
private repository paths, hostnames, credentials, or raw addresses.

```json
{
  "version": "answer-quality-scorecard/v1",
  "run_id": "redacted-local-scorecard",
  "eshu_commit": "0123456789abcdef",
  "prompts": [
    {
      "id": "service-story",
      "family": "service_story",
      "prompt": "Build the service story for service-a and cite it.",
      "expected_truth_class": "deterministic",
      "required_surfaces": ["api", "mcp"],
      "required_next_calls": ["build_evidence_citation_packet"],
      "results": [
        {
          "surface": "api",
          "useful": true,
          "supported": true,
          "answer_summary": "useful redacted service story",
          "truth_class": "deterministic",
          "freshness": "current",
          "citation_handles": ["repo:demo"],
          "next_calls": ["build_evidence_citation_packet"],
          "narration": {
            "status": "not_requested"
          }
        }
      ]
    }
  ]
}
```

The example above is intentionally incomplete because a passing scorecard must
include all seven families. Use `eshu answer-quality-scorecard --json` to keep
the resulting verdict comparable between runs.

Optional narration evidence stays offline. When `narration.status` is
`accepted`, include the governed narration validator input so the scorecard can
run the same sentence-to-provenance checks as the presentation gate. When
`narration.status` is `rejected` or `unavailable`, the deterministic fallback
row must remain publishable and canonical. In all cases, narration must preserve
fallback truth class, freshness, support, partial/truncated state, citations,
limitations, and next calls. The scorer fails rows that drop or weaken any of
those fields.

## Hosted And Full-Stack Proof

Do not claim deployable hosted answer quality from local-only evidence. For
hosted or full-stack workflows, first run the remote Compose or deployed-service
proof that owns the environment, then capture the hosted and CLI answer rows
from that run. Store only the redacted scorecard artifact and the command
outputs that prove the scorecard passed.

If hosted evidence cannot be captured in the current environment, mark that
scorecard incomplete. The scorer should fail rather than invent hosted parity.

## Service intelligence report scorecard

The same gate scores composed [service intelligence reports](../service-intelligence-report.md)
through `answerquality.ScoreReport`. It rejects a report that carries a confident
unsupported claim, a citation gap, a hidden truncation, a missing limitation, an
upgraded truth class, or an unexecutable next call. The share-safe `ReportCorpus`
fixtures (one happy path plus one fixture per failure mode) back the CI and local
dogfood run, so a regression that hides any of these failures fails the build.

## Verification

The scorecard evaluator is unit-tested as a pure Go package and exposed through
the CLI:

```bash
cd go && go test ./internal/answerquality -count=1
cd go && go test ./cmd/eshu -run 'TestAnswerQualityScorecardCommand' -count=1
```

Docs-only scorecard updates still require the strict docs build and
`git diff --check`.
