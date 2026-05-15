# semanticeval Package Guidance

`semanticeval` is a pure scoring package for issue #396. Keep it free of
runtime dependencies.

## Rules

- Do not import Eshu query, storage, graph, MCP, telemetry, or collector
  packages.
- Keep JSON contracts strict with unknown-field rejection.
- Add a focused test before changing scoring formulas or truth-class behavior.
- Do not add model-provider or NornicDB calls here; runners should collect
  results elsewhere and pass handles into this package.
- Preserve `false_canonical_claims = 0` as the hard gate for semantic retrieval
  experiments.
