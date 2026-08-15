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
    compressed IPv6 address and is rejected. C++'s `a::b::c`, Rust's `a::b(-1)`,
    and PHP's `DB::$connection` land there for the same reason. A namespace whose
    first segment is hex but whose second is not (`db::connect`, `ff::Field`,
    `std::vector`, `crate::mod`, `Data::Dumper`) publishes normally: the rule
    requires the whole token to be an address, not to start like one;
  - the compressed-address rule needs a hex digit on at least one side of the
    `::`, so a bare `::` between punctuation publishes: `(::)`, `-::-`, `]::[`,
    and Python's reverse slice `a[::-1]`, whose `-` cannot appear in an address
    at all;
  - a bracketed subscript whose contents are themselves a valid compressed
    address is still rejected, and that is the same gap as `abc::def` rather
    than a separate one. `x[::2]`, `x[::]`, `arr[::3]`, and `path[1::2]` are
    Python slices; `[::2]` is also the address `0:0:0:0:0:0:0:2`. Reading a
    letter before the `[` as a subscript marker was tried and reverted, because
    it publishes every address that follows a word — `client[fd00::1]`,
    `sshd[::1]`, `conn[fd00::1]:7687`, Go's `map[fd00::1:true]` — and
    `AnswerSummary` is model-written narration, so no list of the spellings this
    product emits can be closed;
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
    it can see, not just the first. One honest assignment says nothing about the
    next, so `password: string, password: hunter2` is withheld, as is the
    run-together `password:string;password:hunter2` with no space, quote, or
    comma between the two. Swapping the two makes no difference. The limit is
    the key rule rather than the scan: a key the rule never matches is
    unscreened wherever it sits, which is the next bullet; and
  - a key that runs the word together with a prefix and no separator
    (`PGPASSWORD:`) is not screened. It is shape-identical to `checkPassword:`,
    which has to stay publishable. `DB_PASSWORD:` and `POSTGRES_PASSWORD:` are
    screened — an underscore is a separator.
- Every regex is gated on a cheap substring check. That is a performance
  contract, not a style choice — see `UnsafeString`'s comment for the measured
  6.5x it prevents — so re-run `BenchmarkUnsafeStringHonestCorpus` if a gate
  changes.
- The password scan is one `FindAllStringSubmatch` pass, and that is also a
  performance contract. Nothing caps the string reaching it: `ask_sse.go`
  screens the whole joined model answer, and `answerquality` screens evidence
  values taken from indexed repository content. An earlier version restarted the
  regex at each value it classified, which cost 713ms on a 32KB run of
  assignments against 1.19ms for one pass. `valueCharClass` is what makes one
  pass sufficient — it stops the capture at the separator, leaving it in place as
  the next match's left boundary — so re-run
  `BenchmarkUnsafeStringPasswordRunTogetherScale` across all three of its sizes
  if the capture class or the scan changes. One size cannot show the difference.

## Files

- `guardrail.go` — the `Result`/`Verdict` shape, `ValidateResult`, and
  `UnsafeString` with the address, userinfo, and fragment rules.
- `guardrail_password.go` — the `password:` assignment rule: the pattern, the
  value classifier, and the declaration list.
- `substance.go` — the circular-answer check.

## Related docs

- `go/internal/answerquality/README.md`
- `go/internal/query/README.md`
