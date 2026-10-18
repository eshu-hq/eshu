# Evidence: reducer compat alias removals (#6626 batch 6, bucket 3)

## What changed

Six test-only decode-seam aliases are retired from
`go/internal/reducer/compat_decode.go`; their single caller,
`codedataflow_input_invalid_test.go`, now names the canonical
`schemadecode.Decode*` functions directly:

- `decodeCodeFunctionSource` (canonical `schemadecode.DecodeCodeFunctionSource`)
- `decodeCodeFunctionSummary` (canonical `schemadecode.DecodeCodeFunctionSummary`)
- `decodeCodeInterprocEvidence` (canonical `schemadecode.DecodeCodeInterprocEvidence`)
- `decodeCodeTaintEvidence` (canonical `schemadecode.DecodeCodeTaintEvidence`)
- `decodeCodeDataflowFunction` (canonical `schemadecode.DecodeCodeDataflowFunction`)
- `decodeCodeDataflowScanned` (canonical `schemadecode.DecodeCodeDataflowScanned`)

The `decode_seam_compat3` stanza is fully retired (block deleted, retirement
note left in place); the `decode_seam_compat` stanza shrinks from 15 to 11
entries. New guard `TestBucket3TestOnlyDecodeAliasesRetired`
(`compat_alias_retirement_bucket3_test.go`) parses `compat_decode.go` and
fails if any of the six spellings is re-declared.

Bucket scope: this is the smallest coherent deletion slice on the current
head. A fresh census of all 336 compat aliases (qualified `reducer.<Sym>`
sweep over `go/` plus unqualified sweep over the reducer root) shows no
remaining zero-caller aliases outside open PR #8507's three fragment consts
(`reducerFactBatchInsertPrefix/Source/Conflict`, excluded); the six aliases
above are the only group whose callers are all in one test file. Excluded by
rule: `SharedProjectionIntentRow` (large caller family, goes last as a
coordinated slice), the whole `reducer_fact_write_compat` stanza (open PR
#8507 touches it), and the #8496 crossplane slice (storage/cypher, no reducer
compat overlap). The two non-Go hits for these spellings
(`docs/internal/design/package-restructure.md`,
`go/internal/projector/AGENTS.md`) are past-tense historical prose, not live
references, so no doc repoint is needed.

## Why no regression

No-Regression Evidence: each deleted name is a `var` alias for the identical
`schemadecode` function value, so repointing the twelve call sites changes no
dispatch, decode, quarantine-classification, or graph-write behavior; the
migrated tests assert the same field-level outcomes (`function_uid`,
`source_function_uid`, `function_id`, `kind`, persisted `0.0.0` version
acceptance) through the canonical spelling. RED: new
`TestBucket3TestOnlyDecodeAliasesRetired` failed on the pre-deletion tree
(all six names still declared). GREEN after migration plus deletion:
`TestBucket3TestOnlyDecodeAliasesRetired` plus all five
`TestDecodeCode*` tests pass; `go vet ./internal/reducer/` is clean and
`gofmt` reports no diff on the three touched files. No graph query, queue,
worker, lease, batch, runtime knob, or storage contract changes.

## Observability

No-Observability-Change: no metric, span, status field, route, or log
contract changes; the aliases were compile-time function values with no
emitted signal, so operators keep diagnosing these paths through the existing
reducer spans, input_invalid dead-letter counters, and materialization logs.
