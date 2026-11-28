# Performance Evidence Gate

`scripts/verify-performance-evidence.sh` blocks a PR in CI that touches hot
Cypher, graph writes, queues, workers, leases, batching, or runtime knobs
unless the **GitHub PR description** includes both required markers:

- One performance marker: `Performance Evidence:`, `Benchmark Evidence:`, or
  `No-Regression Evidence:` (an optional parenthetical or bracketed qualifier
  before the colon is accepted, such as `No-Regression Evidence (#1234):`).
- One observability marker: `Observability Evidence:` or
  `No-Observability-Change:`.

Do **not** commit ephemeral evidence files under `docs/internal/evidence/` or
`go/**/evidence-*.md`, and do **not** append PR evidence markers to
`go/**/README.md` or `go/**/AGENTS.md` — `scripts/verify-no-ephemeral-docs.sh`
rejects both. Put raw `EXPLAIN` plans and timing logs in CI workflow artifacts
and record durable architectural invariants in `go/<pkg>/README.md` or
`docs/public/decisions/`.

Verify a PR body draft locally before pushing:

```bash
bash scripts/verify-performance-evidence.sh --pr-body <pr-body-file>
```
