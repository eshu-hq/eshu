# 5693 — semantic.code_hint gets a producer

Validation record for issue #5693, from the #5552 deployed proof.

`semantic.code_hint` had a read contract, a checked-in payload schema, a
fact-kind registry entry, an MCP tool (`list_semantic_code_hints`), a
capability-matrix row and a retention policy. It had no producer anywhere in
the runtime. Every deployed `GET /api/v0/semantic/code-hints` returned an empty
list — correctly formed, and indistinguishable from "this repository has no
hints".

## What was built

`go/internal/semanticcode`, the code-hint twin of `go/internal/semanticdocs`.

It is not an LLM integration and calls no provider. Its input boundary carries
output already produced, parsed and redacted elsewhere. What it owns is the
part that must not vary between providers: the provenance envelope, the stable
fact identity, the policy/redaction/freshness state, and the non-canonical
promotion boundary.

The promotion boundary is enforced rather than documented. `PromotionPolicy` is
not configurable, `CorroborationState` defaults to `uncorroborated`, and a
`confidence: high` hint still ships uncorroborated — confidence is a claim
about the model, corroboration is a claim about the code, and only
deterministic evidence moves the second one.

## Proof

| Check | Command |
| --- | --- |
| Producer contract: admissibility, promotion boundary, replay determinism, fail-closed on untraceable input, conservative defaults | `go test ./internal/semanticcode -count=1` |
| Producer to answer, end to end | `go test ./internal/query -run TestCodeHintsReadAnswersFromTheRealProducer -count=1` |

The second one is the test this issue actually needed. The read handler was
always correct; it had nothing to read, so a handler test alone proved only
that an empty answer is well-formed — which is exactly the shape the deployed
surface returned. That test runs the real producer, hands its envelope to the
read model the way the store does, and asserts the response carries the hint
with `promotion_policy` and `corroboration_state` intact.

Fact identity is keyed on the source content hash, so a hint about code that
has since changed gets a new identity instead of silently superseding under the
old key. `TestEmitIsDeterministicForTheSameProviderOutput` covers both halves.

## What is still not true

**No runtime process calls this producer.** That is the honest limit of this
change, and it is why the deployed capability stays `experimental`.

The sibling package is in the same position: `go/internal/semanticdocs` has
zero consumers outside its own package, and the `production: supported` claim
for `semantic_evidence.documentation_observations.list` rests on a
remote-validation artifact whose entire committed evidence is
`go test ./internal/query` — handler-level tests, no deployed artifact and no
producer. That is a separate capability from this issue's and is left alone
here, but it is recorded because the two claims stand or fall on the same
missing piece.

Because nothing emits code hints in the corpus, the golden gate's
`list_semantic_code_hints` shape keeps `minimum_results: 0`. The snapshot now
says so explicitly, with the condition that would raise it, so the floor is a
documented decision rather than a vacuous assertion nobody revisits.

## No-Regression Evidence:

The producer is additive and unreferenced by any runtime path, so no existing
behavior changes: baseline is the current runtime with no `semantic.code_hint`
facts, and the after state is identical because nothing calls
`semanticcode.Emit` outside tests. Terminal counts are therefore unchanged —
the corpus emits zero code-hint facts before and after. Verified by
`go test ./internal/query ./internal/facts ./internal/semanticcode
./internal/capabilitycatalog ./cmd/capability-inventory -count=1`.
Backend/version: no backend involved; the producer builds fact envelopes in
process and performs no I/O.

## No-Observability-Change:

No new metric. The package performs no I/O and runs no goroutines: it builds
envelopes and returns them. When a runtime process eventually calls it, the
facts it produces flow through the same `eshu_dp_facts_emitted_total` and
`eshu_dp_generation_fact_count` instruments every collector's output does, and
that wiring is where the observability question belongs — this package emits no
signal of its own because it has no lifecycle of its own.

## Related

- Issues: #5693, from the #5552 deployed proof; parent #5344.
- `go/internal/semanticdocs` — the documentation twin this mirrors.
