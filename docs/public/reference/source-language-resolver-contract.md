# Source-Language Resolver Contract

This contract defines the proof bar for source-language call and relationship
resolution work. It applies before adding a new resolver plug-in, promoting
SCIP-backed evidence, or raising a language support claim beyond parser-only
facts.

Source-language resolution is part of the facts-first pipeline:

```text
repository source -> parser or SCIP evidence -> content facts -> reducer
admission -> graph projection -> HTTP API and MCP reads
```

Parsers and SCIP adapters may emit source facts. They do not decide canonical
graph truth by themselves. Reducer admission and read-surface tests prove what
can be presented as supported query behavior.

## Resolver Boundary

Each resolver implementation must have one language-owned entrypoint and one
registry entry. Shared parser or reducer code may dispatch through the registry,
but it must not contain language-specific naming branches for every framework,
runtime, or import form.

The resolver output must be deterministic for the same repository bytes,
configuration, and SCIP input. It must not depend on map iteration order,
timestamps, network calls, local machine paths, random IDs, or mutable global
state.

Resolvers may emit these classes of evidence:

| Evidence class | Meaning |
| --- | --- |
| `direct` | Syntax proves the subject, object, and relationship in the same parse scope. |
| `corroborated` | Parser and SCIP, import, type, framework, or package evidence agree on the relationship. |
| `ambiguous` | Multiple targets or receivers remain plausible and no winner is proven. |
| `unsupported` | The language, framework, dynamic dispatch form, or generated-code boundary is not modeled. |

Only `direct` and `corroborated` evidence may feed canonical relationship
projection, and only after reducer tests prove the admission rule. Ambiguous and
unsupported evidence can be surfaced as reviewable limitations; it must not be
converted into a guessed canonical source edge. This includes `CALLS`,
`IMPORTS`, `REFERENCES`, `IMPLEMENTS`, `INHERITS`, `OVERRIDES`, `ALIASES`,
`INSTANTIATES`, `USES_METACLASS`, or framework-specific edge families.

## SCIP Corroboration

SCIP is the precise external index path for supported languages, but it is not a
replacement for Eshu's truth contract. Enabling or promoting SCIP evidence must
state:

- the dominant-language detection rule and mixed-language behavior
- the external binary name and failure mode when the binary is missing
- the parser facts SCIP corroborates or supersedes
- the exact edge kinds SCIP can promote
- the unsupported dynamic, reflective, generated, or framework cases

A missing SCIP binary, empty SCIP result, or unsupported language must leave the
native parser behavior unchanged and observable. It must not silently fabricate
edges from names alone.

## Golden Audit Fixtures

Every resolver plug-in or support-promotion PR must include an audit fixture
whose expected graph is authored independently from Eshu's own output. The
fixture shape is:

```text
fixtures/source_language/<language>/<case>/
  src/...                 # minimal source project
  goldens/nodes.jsonl     # source-authored expected entities
  goldens/edges.jsonl     # source-authored expected relationships
  README.md               # intent, unsupported cases, and verifier command
```

Goldens must be reviewed source truth, not a saved copy of parser, reducer,
API, MCP, or graph output. Self-comparison is invalid evidence because it proves
repeatability, not correctness.

The audit should score:

| Metric | Requirement |
| --- | --- |
| Node recall | Expected source entities are present with stable handles. |
| Edge recall | Expected relationships are present with the documented edge kind. |
| Edge precision | Unexpected guessed edges fail the audit unless explicitly listed as allowed noise. |
| Ambiguity preservation | Ambiguous cases remain absent from canonical edges or marked as ambiguous evidence. |
| Unsupported behavior | Unsupported dynamic or generated-code cases return a documented limitation, not a fallback edge. |

Regression thresholds belong in the verifier and must be strict enough to fail
on a removed expected edge or a newly fabricated canonical edge. Until a
dedicated source-language golden verifier exists, resolver implementation PRs
must add one in the same change or document the existing command that loads
`fixtures/source_language/**/goldens`, scores `nodes.jsonl` and `edges.jsonl`,
and fails fabricated or missing canonical edges.

## Required Cases

Each resolver implementation must cover positive, negative, and ambiguous
fixtures for the exact language feature being claimed.

Positive cases prove the intended relationship:

- same-file and cross-file direct calls
- imports or package exports that bind to a single target
- supported framework roots, callbacks, lifecycle hooks, or public API shapes
- supported receiver, interface, trait, protocol, inheritance, or overload forms

Negative cases prove Eshu does not over-claim:

- similarly named but unrelated symbols
- imports that resolve outside the indexed repository or supported scope
- generated code with no source-to-generated mapping
- dynamic loading, reflection, macros, dependency injection, or plugin systems
  that the resolver does not model

Ambiguous cases prove no alphabetical or first-match fallback:

- multiple same-name targets with no type or import discriminator
- overloaded functions without enough parameter or receiver evidence
- structural interface, trait, or protocol matches with multiple valid
  candidates
- mixed-language or mixed-package repositories where SCIP selects one dominant
  language and parser evidence for another remains parser-only

## Read-Surface Promotion

A resolver feature is not supported on HTTP API, MCP, repository story,
language-query, or dead-code surfaces until a query or integration test proves
that read path. Parser rows alone are source evidence only.

Promotion requires:

1. Parser or SCIP fixture proof for emitted evidence.
2. Reducer admission proof for any canonical graph edge.
3. Query or MCP proof for every surfaced response field or relationship kind.
4. Public language page and support matrix updates that name exact limits.
5. A docs build and parser relationship kit verification.

Read responses must preserve truth labels, limits, deterministic ordering, and
truncation behavior. Unsupported resolver capabilities should return documented
limitations rather than low-authority answers that look complete.

## Performance And Observability

Resolver implementation PRs must include a durable evidence note when they
change parser, SCIP, reducer, graph-write, or read-path behavior.

Use one of:

- `Performance Evidence:` for before/after runtime proof.
- `Benchmark Evidence:` for focused resolver or reducer benchmarks.
- `No-Regression Evidence:` for correctness-only changes with same-shape proof.

And one of:

- `Observability Evidence:` when new or existing signals prove the path.
- `No-Observability-Change:` when existing parser, reducer, graph, query, or
  status signals already diagnose the path.

For docs-only contract changes, state that no runtime code, graph query, queue,
worker, or parser behavior changed.

## Verification Gate

At minimum, source-language resolver contract or implementation changes run:

```bash
scripts/verify-parser-relationship-kit.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

The verification evidence must also include a targeted sensitive-marker scan
over every changed public doc and navigation file. Investigate any output before
merge; do not commit private group identifiers or attribution trailers.

Implementation PRs add focused Go tests for the touched parser, reducer, query,
or MCP packages. They also run or introduce the source-language golden audit
verifier described above. If the implementation changes graph writes, queues,
workers, leases, batching, or hot-path query behavior, also run the performance
evidence gate from the local testing reference.

No-Regression Evidence: this contract is documentation-only. It changes no
parser output, SCIP detection, reducer admission, graph write, queue, worker, or
read-path behavior.

No-Observability-Change: this contract adds no runtime behavior. Future
implementation PRs must name the existing or new parser, reducer, graph, query,
status, metric, log, or trace signal that proves their path.
