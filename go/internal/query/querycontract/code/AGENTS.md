# Agent instructions: querycontract/code

Contract-only package. Types and pure helpers, no policy.

## Before changing anything here

`DeadCodeCandidateLabels` is a wire contract, not an internal list. The
OpenAPI `candidate_kind` enum is pinned to it by a contract test in
`internal/query`. Adding or removing a label changes what the API advertises
and what the scan checks together; do both deliberately or neither.

`DeadCodeCandidateEntityType` must agree with `DeadCodeCandidateLabels`. They
are two spellings of one set. `TestDeadCodeCandidateEntityTypeMapsEveryAdvertisedLabel`
(in `codequery/deadcode`) fails when a label is added without a mapping, but
nothing fails when a label is removed and its `switch` arm is left behind.
Edit both together.

## The rule that keeps this package extractable

Outbound imports are `strings` and nothing else. Do not add an Eshu import.
The moment this package imports its parent or a sibling, the cycle that forced
these types down here reappears, and the callers that cannot see each other
lose their shared vocabulary.

Verify after any change:

```
cd go && go build ./internal/query/... && go vet ./internal/query/...
```

`go build` skips `_test.go`; `go vet` is what catches a test file left behind.

## Naming

Identifiers keep the `DeadCode` prefix. It is not stutter against the package
name: "dead code" is a single term, and this package is the code family's
contract surface, so the dead-code subset stays marked for the broader surface
that joins it later.
