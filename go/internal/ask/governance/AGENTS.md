# Agent Instructions: go/internal/ask/governance

## Scope

This package is the **governed-narration posture resolver** for Ask Eshu. It
computes `status.AnswerNarrationStatus` (the value the engine consumes via
`Engine.SetNarrationPosture`) and defines bounded audit-safe telemetry labels.

## Read-First Order

Before editing any file here:

1. `doc.go` — package contract, security references, wiring pattern.
2. `governance.go` — `PostureInputs`, `ResolvePosture`.
3. `telemetry.go` — `AskOutcome`, `AskStage` and their cardinality contracts.
4. `go/internal/status/answer_narration.go` — the exact state/reason/retention
   consts that `ResolvePosture` maps to. Never invent new string literals.
5. `go/internal/ask/engine/narration.go` — how `SetNarrationPosture` is consumed.

## Invariants (enforce continuously)

1. **Default-closed.** `ResolvePosture(PostureInputs{}, now)` MUST NOT return
   `State == AnswerNarrationAvailable`. All five gates must be `true`.

2. **DeterministicFallbackAvailable always true.** Every code path in
   `ResolvePosture` MUST set `DeterministicFallbackAvailable = true`.

3. **CanonicalTruthAffected always false.** Every code path MUST set
   `CanonicalTruthAffected = false`.

4. **RetentionPosture always metadata_only.** Never set a different value.

5. **Leak-safe.** `PostureInputs` holds only boolean flags. It MUST NOT hold
   credentials, question text, provider bodies, or tenant identifiers.

6. **Low-cardinality telemetry.** `AskOutcome` and `AskStage` MUST remain
   bounded enums. Do not encode high-cardinality data in them.

7. **Use status consts verbatim.** All state/reason/retention values come from
   `go/internal/status/answer_narration.go`. Do not hardcode string literals.

## Security scope

This package is in the Tier-2 security review scope (#1755/#1900/#1902). Any
change to `PostureInputs` or `ResolvePosture` MUST be flagged for the
security review.

## Verification gates

```bash
cd go
gofmt -l ./internal/ask/governance          # must print nothing
go vet ./internal/ask/governance
golangci-lint run ./internal/ask/governance/...
go test ./internal/ask/governance -count=1
```

All four must pass before a PR is opened.
