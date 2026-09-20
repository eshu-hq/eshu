# #6695 contracttest leaf: contracttest → conformance/contract move evidence

Rename-only nest per #6695 (`collector/contracttest` becomes
`collector/conformance/contract`, package `contract`) joining `parity`
under the `conformance/` parent. Type `Contract` renamed to
`CollectorContract` (rule 4: no `contract.Contract` package stutter);
`Contracts()` kept (plural descriptive, no repeat). Generated
`contract_data.go` renamed to `data.go` (rule 2: the move made the old
name repeat its directory); `contracttest.go`/`contracttest_test.go`
renamed to `assert.go`/`assert_test.go` (rule 1: glued compound). The `gen/` command
becomes `contract-gen` with its default outPath, template package clause,
and emitted type literals repointed; `generate-contracttest.sh` /
`verify-contracttest.sh` internal paths repointed (script filenames kept:
the CI gate registry references them). Sole external user (s3 contract
test) repointed. Generator round-trip proven: regenerate writes the
committed file byte-identically and `-check` passes; the gen unit test
expectations updated to the new emitted names.

No-Regression Evidence: `go test -count=1` on contract, gen, parity, and
the s3 importer tests, baseline origin/main `9621f344` vs leaf head,
toolchain go1.27.1 darwin/arm64, same input shape (unit suites plus
spec-driven generation): baseline contract ok and gen ok; after move all
three conformance packages ok plus the s3 `-run Contract` suite ok.
`go test -list` discovers 17 tests on both sides (identical set modulo
the package path). `go vet` clean recursive. Safe because the move is
byte-identical except the package clause, import path/qualifiers, the
`CollectorContract` rename, and template output names; no logic, spec,
or wire change.

No-Observability-Change: the harness and generator emit no facts,
metrics, spans, logs, status rows, or graph writes at runtime (the
generator runs at build time only); no telemetry-coverage row names
this package.
