# Code projector domains

This namespace groups projector intent builders backed by source-code analysis.
The leaves select facts and construct reducer intents; root projector assembly
continues to own ordering, queue writes, retries, and telemetry.

- `function/summary` builds function-summary persistence intents.
- `interproc/evidence` builds cross-function evidence intents.
- `taint/evidence` builds taint-evidence intents.

The namespace adds no runtime ownership and is not an independently extractable
service boundary. The leaves still depend on internal Eshu contracts.
