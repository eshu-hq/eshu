# AGENTS.md — fact decode seam guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `sdk/go/factschema/` for the typed values these decoders produce.
4. `specs/fact-kind-registry.v1.yaml` for the kind-to-decoder mapping.

## Invariants

- **This package is a leaf.** It must not import `../canonical`, `../runtime`,
  `../stage`, `../failure`, or the root `projector` package. Every one of them
  depends on it, so a single import back closes a cycle. This is why the
  canonical stage labels live in `stage_label.go` here rather than with the
  extractors that report them.
- `PartitionFailures` has exactly two branches: an absent required payload
  field is `input_invalid` and comes back as a `QuarantinedFact`; every other
  decode error is returned fatally. Do not add a third, and do not let either
  become a silent skip — a swallowed decode failure produces a graph quietly
  missing rows.
- The stage labels are a bounded metric dimension on
  `eshu_dp_projector_input_invalid_facts_total`. Adding an unbounded value
  makes the metric unusable at repo scale. A new typed family adds one entry to
  `quarantinedFactStagePrefixes`, longest-prefix-first, rather than another
  branch.
- A new typed decoder is a contract change: update
  `specs/fact-kind-registry.v1.yaml` and the SDK in the same PR, and use
  `eshu-contract-rigor`.
- `fact_kind.go` is fact-kind selection, not a stage, despite arriving as
  `stage_facts.go`. `../canonical` uses it as heavily as `../stage` does.

## Verification

```bash
cd go && go test ./internal/projector/decode/... -count=1
cd go && go test ./internal/projector/canonical/... ./internal/projector/runtime/... -count=1
```

A payload-shape change also needs `scripts/verify-factschema-diff.sh`, which
`make pre-pr` does not run.
