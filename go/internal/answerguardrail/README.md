# Answer Guardrail

## Purpose

`answerguardrail` owns pure output checks for publishable answer text. Runtime
Ask Eshu and the offline answer-quality scorecard use it to reject supported
answers without citations, strings that look like private paths, hosts,
credentials, or raw addresses, and circular identity-only answers that only
restate the question's entity and name no operational fact.

## Ownership boundary

This package owns deterministic string and citation guardrails only. It does not
call API, MCP, graph, Postgres, providers, telemetry, or redaction services, and
it does not decide whether a route or provider may run.

## Exported surface

- `Result` — bounded answer fields evaluated by guardrails (including the
  `Question` used by the answer-substance check).
- `ValidateResult` — evaluates citation coverage, publish safety, and, when a
  `Question` is supplied, answer substance (circular / identity-only rejection).
- `IsCircularAnswer` — deterministic detector for a tautological, identity-only
  answer that only restates the question's entity; shared by the runtime handler
  and the offline answer-quality scorer.
- `Verdict`, `Finding`, `Criterion` — stable result types for callers
  (`CriterionCitationCoverage`, `CriterionPublishSafety`,
  `CriterionAnswerSubstance`).
- `FirstUnsafeString`, `UnsafeString` — deterministic publish-safety scanner
  used by scorecard code that needs the first rejected value.

See `doc.go` for the godoc contract.

## Dependencies

Only the Go standard library. This keeps the package safe for both runtime
query code and offline scorecard code without import cycles.

## Telemetry

No telemetry is emitted. Callers decide how to surface guardrail findings in
their own logs, status, responses, or scorecards.

## Gotchas / invariants

- Findings must not echo the rejected value; runtime callers may put findings
  directly in user-visible limitations.
- `ValidateResult` requires citations only for supported answers with a
  non-empty published summary. Unsupported fallback rows may explain their
  limitation without citations.
- The answer-substance check runs only for a supported answer with a non-empty
  `Question`; an empty `Question` disables it. It flags an answer whose content
  tokens are all drawn from the question (an identity restatement) and passes any
  answer that introduces a new operational token.
- The scanner is intentionally conservative and deterministic. Do not add
  network, filesystem, or provider-dependent checks here.
- `UnsafeString` runs on a publish path, so its rules are tuned against false
  positives as hard as against coverage: an answer it wrongly withholds is its
  own outage. Each rule's comment in `guardrail.go` names what it deliberately
  does not catch. The limits worth knowing before relying on it:
  - an all-hex identifier pair such as `abc::def` is shape-identical to a
    compressed IPv6 address and is rejected. A namespace whose first segment is
    hex but whose second is not (`db::connect`, `ff::Field`) publishes normally:
    the rule requires the whole token to be an address, not to start like one;
  - only `password` gets a colon-spelled rule. `token`, `secret`, and `api_key`
    keep their `=` form only, because real resource types end in those words
    (`aws_appsync_api_key`, `aws_secretsmanager_secret`) and a colon rule on
    them would reject honest answers;
  - the password rule reads the value, not the keyword, so `password: string`,
    `password: String!`, and `random_password: 3 resources` — a schema field and
    a Terraform resource count — stay publishable. The cost is that a password
    that is only digits (`password: 123456`) or only punctuation reads as a
    count or a placeholder and is not screened. `evidencebundle`'s
    `credentialPattern` documents the same gap for the same reason;
  - because the value decides, the rule classifies every `password:` assignment
    in a string, not the first one. One honest assignment says nothing about the
    next, so `password: string, password: hunter2` is withheld and the order the
    two appear in does not change the answer; and
  - a key that runs the word together with a prefix and no separator
    (`PGPASSWORD:`) is not screened. It is shape-identical to `checkPassword:`,
    which has to stay publishable. `DB_PASSWORD:` and `POSTGRES_PASSWORD:` are
    screened — an underscore is a separator.
- Every regex is gated on a cheap substring check. That is a performance
  contract, not a style choice — see `UnsafeString`'s comment for the measured
  6.5x it prevents — so re-run `BenchmarkUnsafeStringHonestCorpus` if a gate
  changes.

## Related docs

- `go/internal/answerquality/README.md`
- `go/internal/query/README.md`
