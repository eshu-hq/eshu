# Link Prediction Candidates

Link-prediction candidates are diagnostic relationship suggestions. They are not
canonical Eshu relationships, resolver output, graph edges, or API/MCP response
contracts.

The contract lives in `go/internal/linkcandidates`. It gives issue #420 a
bounded internal proof path before live NornicDB link-prediction procedures,
query surfaces, reducers, or telemetry exporters use candidate suggestions.

## Candidate Shape

Each candidate records:

| Field | Meaning |
| --- | --- |
| `id` | Stable candidate evidence id. |
| `algorithm` | Low-cardinality algorithm token up to 64 characters, using lowercase letters, digits, `.`, `_`, or `-`, such as `nornicdb.adamic_adar`. |
| `score` | Finite score from `0` to `1`. |
| `source` | Bounded source graph handle with `kind` and `id`. |
| `target` | Bounded target graph handle with `kind` and `id`. |
| `evidence_context` | Short explanation of the evidence neighborhood. |
| `freshness` | State plus observed timestamp for candidate input. |
| `reason` | Human-readable reason for the decision. |
| `truth_level` | `candidate` or `semantic_candidate`. |
| `decision` | `generated`, `suppressed`, or `ambiguous`. |

Truth labels outside `candidate` and `semantic_candidate` are rejected. Link
prediction must not emit `exact`, `derived`, `canonical`, or other labels that
imply relationship admission.

## Decisions

| Decision | Meaning |
| --- | --- |
| `generated` | Candidate is visible as diagnostic evidence. |
| `suppressed` | Candidate was withheld, but counted and explainable. |
| `ambiguous` | Candidate remains provenance-only because the source or target is not decisive. |

Ambiguous candidates must not choose a winner. Future canonical admission must
be reducer-owned and covered by a separate design.

## Freshness

Freshness states are:

- `fresh`
- `stale`
- `building`
- `unavailable`

Every candidate must include `observed_at`. Missing freshness is invalid
because stale candidate neighborhoods can otherwise look like current runtime
truth.

## Observation Contract

`ObservationFor` returns only:

- algorithm;
- decision.

Do not use source handles, target handles, repository ids, service ids,
candidate ids, or evidence ids as metric labels. Put those values in evidence
records or logs where bounded drilldown is appropriate.

## Evaluation Gate

Issue #420 evaluation uses `EvaluateCandidates` from
`go/internal/linkcandidates`. The gate compares generated candidates with a
fixture-defined expected-gap set and keeps suppressed or ambiguous candidates
visible in the counts. Empty expected-gap fixtures are invalid, and duplicate
generated candidates for the same expected gap do not improve precision.

Each evaluation records:

- candidate count;
- generated, suppressed, and ambiguous counts;
- matched expected gap count;
- false positive count;
- precision and recall;
- decision counts by algorithm and decision.

Precision and recall are experiment metrics only. They do not admit
relationships into resolver output or the canonical graph. Invalid candidates,
including candidates with canonical truth labels, fail validation before they
can be counted.

## Relationship Mapping Boundary

Candidate suggestions feed evaluation and investigation only. The relationship
mapping flow remains:

```text
evidence -> resolver admission -> resolved relationship rows -> reducer graph writes -> query stories
```

`linkcandidates` does not participate in resolver admission or graph writes.
If a future slice admits link-prediction suggestions into canonical
relationships, that slice must define reducer-owned admission, rejection,
rollback, graph proof, query proof, and telemetry separately.

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/linkcandidates ./internal/searchdocs -count=1
```

Docs changes must also pass:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Related Docs

- [Relationship Mapping](relationship-mapping.md)
- [Relationship Evidence And Resolution](relationship-mapping-evidence.md)
- [Search Benchmark Evidence](search-benchmark-evidence.md)
