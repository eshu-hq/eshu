# ask/governance

Package `governance` implements the **governed-narration posture resolver** and
**audit-safe ask observability vocabulary** for Ask Eshu.

## Purpose

Ask Eshu answer narration is **default-CLOSED**. This package is the single
place where the enable-gate decision is computed. The `engine` package consumes
the resolved `status.AnswerNarrationStatus` via `Engine.SetNarrationPosture`.

## Default-closed model

```
ResolvePosture(PostureInputs{}) → State=unavailable, Reason=disabled_by_default
```

All five gates must be `true` for narration to be permitted:

| Gate                   | Closed reason           |
|------------------------|-------------------------|
| ProviderConfigured     | provider_unavailable    |
| ProviderTrafficEnabled | provider_unavailable    |
| PolicyAllowed          | policy_denied           |
| BudgetAvailable        | budget_exhausted        |
| PublishSafetyEnabled   | disabled_by_default     |

Priority: provider gates check first, then policy, then budget, then safety.

## Invariants

- `DeterministicFallbackAvailable` is **always** `true` — the deterministic
  answer packet path is never gated.
- `CanonicalTruthAffected` is **always** `false` — narration is an optional
  layer on top of canonical packets; it never changes the truth they carry.
- `RetentionPosture` is **always** `metadata_only` — no prompt or response
  bodies are retained.

## Audit-safe telemetry vocabulary

`AskOutcome` and `AskStage` are bounded low-cardinality enums used as metric
and span labels. They MUST NOT encode question text, provider bodies, or tenant
identifiers. See the cardinality contract in `telemetry.go`.

## Wiring pattern

```go
posture := func() status.AnswerNarrationStatus {
    return governance.ResolvePosture(buildInputs(), time.Now())
}
engine.SetNarrationPosture(posture)
```

`buildInputs()` reads live configuration and returns a `PostureInputs`. It is
the caller's responsibility; this package does not read configuration directly.

## Security references

- **ADR #2462** — governed narration design. This package is the enable-gate
  implementation referenced by that ADR.
- **Issues #1755 / #1900 / #1902** — Tier-2 security review. Any change to
  `PostureInputs` or `ResolvePosture` logic is in Tier-2 scope and MUST be
  included in the security review.

## Verification

```bash
cd go
gofmt -l ./internal/ask/governance
go vet ./internal/ask/governance
golangci-lint run ./internal/ask/governance/...
go test ./internal/ask/governance -count=1
```
