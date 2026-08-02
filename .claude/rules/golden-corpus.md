---
paths:
  - "testdata/cassettes/**"
  - "testdata/golden/**"
  - "go/cmd/golden-corpus-gate/**/*.go"
  - "scripts/verify-golden-corpus-gate.sh"
  - "scripts/lib/golden-corpus-*.sh"
---

# Golden corpus gate, cassettes, and the B-12 snapshot

**Load `eshu-golden-corpus-rigor`.** Add `eshu-contract-rigor` when a fact kind
or payload shape moves with it.

The cassettes and the snapshot are one artifact in two files. A change that moves
collector facts, reducer output, or a query response shape has to move both, in
the same PR, or the gate asserts against a truth that no longer exists.

`minimum_results` binds only when `results_field` names a top-level response key.
An envelope that nests its array under `data` needs `required_json_paths` with
`[]` segments instead — a positive `minimum_results` without a matching
`results_field` is a hard error, not a silent no-op.
