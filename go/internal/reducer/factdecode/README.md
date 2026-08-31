# Reducer fact decode

## Purpose

`factdecode` owns what happens when a fact payload cannot be decoded: how the
failure is classified, whether it retries, and how a single malformed fact
becomes a durable dead letter instead of failing a whole intent.

It sits below both the reducer root and the domain-family packages for the same
import-direction reason as `payloadcore`: root imports families to construct
their handlers, so a family cannot import root back, and the quarantine
mechanism is needed on both sides (issue #6061, epic #6053).

## Ownership boundary

This package owns the mechanism:

- `FactDecodeError` — wraps a classified `*factschema.DecodeError` so the
  Postgres queue's failure path reads it through `errors.As` and treats it as
  terminal. `Retryable` returns false: a missing required field cannot succeed
  on replay unchanged.
- `QuarantinedFact` — the in-flight per-fact dead letter, with exported fields
  so family packages can build one.
- `PartitionDecodeFailures` — splits a quarantinable `input_invalid` fact from a
  fatal error, so the batch keeps projecting every valid fact.
- `RecordQuarantinedFacts` / `QuarantinedFactWriter` / `WithQuarantineWriter` —
  persistence, with the writer carried on the context.
- `InputInvalidSubSignals` — the operator-facing sub-signal breakdown.

It does NOT own the per-fact-kind decoders. `decodeAWSResource`,
`decodeCICDRun`, `decodeOCIImageIndexForIndex` and their siblings decode a
specific fact kind and belong to the family that owns that kind; they stay in
the reducer root until their family moves.

## Exported surface

| symbol | role |
|---|---|
| `FactDecodeError`, `NewFactDecodeError` | typed decode failure carrying its fact kind and failure class |
| `PartitionDecodeFailures` | splits a decode batch into the facts that survived and the ones to quarantine |
| `QuarantinedFact`, `QuarantinedFactRecord` | the quarantined fact and the durable row shape written for it |
| `QuarantinedFactWriter`, `WithQuarantineWriter` | the persistence port and the context carrier the Service stashes it on |
| `RecordQuarantinedFacts` | the choke point: counts, logs, and best-effort persists a quarantine batch |
| `QuarantinedAttributeShapeFact`, `AttributeShapeAsFactDecodeError` | attribute-shape adapters (see Gotchas) |
| `InputInvalidSubSignals` | the sub-signal breakdown reported alongside the counters |

## Dependencies

`context`, `errors`, `fmt`, plus `sdk/go/factschema` and `sdk/go/factschema/aws/v1`. It imports nothing from the reducer root, and nothing from a domain-family subpackage — that direction is what makes the family moves compile.

## Telemetry

`RecordQuarantinedFacts` emits four instruments, all on the reducer input-invalid path:

| metric | meaning |
|---|---|
| `eshu_dp_reducer_input_invalid_facts_total` | facts quarantined, by fact kind and failure class |
| `eshu_dp_reducer_input_invalid_facts_committed_total` | quarantine rows the writer accepted |
| `eshu_dp_reducer_input_invalid_fact_write_batch_size` | rows per persistence attempt |
| `eshu_dp_reducer_input_invalid_fact_write_errors_total` | persistence attempts that failed |

Rows are in `docs/public/observability/telemetry-coverage.md`. An operator seeing the first counter rise with the second flat is looking at a writer that is failing or absent, not at a decode problem.

## Gotchas / invariants

- **Persistence is best effort and deliberately non-fatal.** A writer error is counted and logged; it never fails the intent. A nil writer is a no-op. Both are locked by tests.
- **The attribute-shape adapters drag in the AWS schema.** `QuarantinedAttributeShapeFact` and `AttributeShapeAsFactDecodeError` import `sdk/go/factschema/aws/v1`, which is the one place this otherwise family-agnostic package names a single family's schema. That coupling is relocated here, not introduced; extracting it is tracked rather than done in this move.
- **`FactDecodeError.factKind` stays unexported.** The exported `FactKind` field belongs to `QuarantinedFact`, a different type. Nothing serializes either, so exporting the record's fields changed no wire output.

## Related docs

- `docs/internal/design/package-restructure.md` — the split this package is part of (#6061)
- `docs/public/observability/telemetry-coverage.md` — the rows for the four metrics above
- `docs/internal/evidence/4630-input-invalid-quarantine-read-surface.md` — why the quarantine read surface exists

## Compatibility

The reducer root keeps type aliases (`quarantinedFact`, `factDecodeError`,
`QuarantinedFactRecord`, `QuarantinedFactWriter`) and function forwarders in
`quarantine_compat.go`, so root call sites and the external packages that name
the exported quarantine types are unchanged. `internal/storage/postgres`
implements `reducer.QuarantinedFactWriter` and constructs
`reducer.QuarantinedFactRecord`; both keep working through those aliases.

## Field naming

`QuarantinedFact`'s fields were unexported while the type lived in the reducer
root. They are exported here because three files outside this package construct
the value with field literals. `FactDecodeError.factKind` stays unexported — it
is internal to the error's own message and nothing outside constructs one
directly.
