# answerquality

## Purpose

`answerquality` defines the offline dogfood scorecard for representative Eshu
answers. It scores captured, redacted API, MCP, CLI, and hosted results for
usefulness, truth honesty, citation coverage, boundedness, optional narration
fallback preservation, parity, follow-up usefulness, and publish safety.

## Boundary

This package does not call live API or MCP endpoints. Operators and tests
capture answers from the real surfaces, redact them, then pass the evidence to
`Score`. That keeps private paths, hostnames, credentials, raw addresses, and
sensitive excerpts out of versioned artifacts.

## A publish-safety failure names the field, never the value

`publish_safety` is the one criterion whose input is, by definition, something
that must not be published. Its `Detail` therefore names where the value was
found (`unsafe publishable evidence in api result answer_summary`) and never
what it was. This is the same contract `answerguardrail.Finding` documents for
itself, and the reason it matters here is that a `CriterionScore.Detail` is not
a local diagnostic: it is printed to stdout, returned in the CLI's error to
stderr, serialized into the `--json` verdict, and copied verbatim into a
generated `FollowUpIssue` body.

`Verdict.RunID` follows the same rule. A run id that itself fails the scan is
replaced by `RedactedRunID`, because the CLI prints it in the scorecard header
and it is carried in the JSON artifact.

Use `locatedString` and `firstUnsafeLocation` when adding a value to the
screened set, not `answerguardrail.FirstUnsafeString` — the latter returns the
value, which is what this contract exists to keep out of the output.

## Evidence

`EvidenceVersion` is `answer-quality-scorecard/v1`. A complete run must include
one prompt from each default family:

- service story
- code-topic investigation
- incident context
- supply-chain impact
- documentation truth
- freshness/readiness
- hosted onboarding/governance

## Service intelligence report scorecard

`ScoreReport` extends the dogfood gate to composed service intelligence reports
(`serviceintel.Report`). `ReportEvidenceVersion` is
`service-intelligence-report-scorecard/v1`. It scores a report against six
criteria and fails a report that ships polished but dishonest:

- `unsupported_claim_avoidance` — no confident summary on an unsupported or
  evidence-less section;
- `citation_coverage` — every supported claim names an evidence handle or
  citation;
- `truth_class_preservation` — no upgraded or invented truth class;
- `limitation_visibility` — every partial or unsupported section says why;
- `truncation_signaling` — truncation is marked partial and stated, not hidden;
- `next_call_executability` — every recommended next call and suggested
  investigation names a real tool, route, or playbook.

`ReportCorpus` is a share-safe fixture corpus: one honest happy path, one honest
partial report, and one fixture per failure mode. It backs the CI and local
dogfood gate so a regression that hides truncation, drops citations, or lets a
confident unsupported claim through fails the build.

No-Observability-Change: scorecard evaluation is an offline pure function over
captured evidence or a composed report. It starts no Eshu runtime, opens no
API/MCP transport, reads no graph/Postgres driver, and emits no OTEL signal.

Optional narration evidence is scored as a comparison against the deterministic
fallback row. Accepted narration must pass `answernarration.Validate`, and every
captured narrated row must preserve fallback truth class, freshness, support,
partial/truncated state, citations, limitations, and next calls.

No-Regression Evidence: scorecard evaluation is covered by
`go test ./internal/answerquality -count=1`.
