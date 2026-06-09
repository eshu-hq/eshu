# Semantic Guard

## Purpose

`semanticguard` applies deterministic security gates to one semantic extraction
chunk or provider response before prompt construction, provider egress, or
retention. It turns policy, ACL, extractor, classification, redaction,
prompt-safety, and retention evidence into one reason-coded decision.

## Ownership boundary

This package owns pure guard evaluation for #1849. It does not parse provider
profiles, decide source allowlists, load credentials, construct prompts, enqueue
jobs, persist audit rows, emit telemetry, or call hosted or local providers.

## Exported surface

- `Assessment` - chunk-level guard input supplied by future queue or provider
  workers.
- `ResponseAssessment` - provider-output guard input supplied after a schema
  parser validates the response envelope.
- `Decision` - audit-safe result with stable state, reason, redaction summary,
  source and chunk hashes, and optional prompt-safe redacted text.
- `Evaluate` - applies provider/profile/policy, ACL, extractor, budget,
  classification, redaction, prompt-injection, and retention gates.
- `EvaluateResponse` - applies provider-response schema, classification,
  prompt-safety, hash, and retention gates.

See `doc.go` for the godoc contract.

## Dependencies

Standard library only. Callers map decisions from `internal/semanticpolicy`,
profile status from `internal/semanticprofile`, and future queue or audit rows
into this package's plain structs.

## Telemetry

None directly. Future queue, provider, API, or MCP callers must emit bounded
metrics, spans, logs, and audit rows from `Decision.State`, `Decision.Reason`,
source class, provider profile class, retention posture, and safe hashes. Raw
paths, prompts, responses, provider request ids, tenant names, customer names,
credential handles, and source identifiers must not become metric labels.

## Gotchas / invariants

- The guard denies by default. Missing policy allow, stale ACLs, unsupported
  extractors, missing classifiers, unknown classes, unsafe redaction, prompt
  injection indicators, oversized chunks, and unsafe retention all stop work.
- `PromptSafeText` is populated only on an allowed chunk preflight decision. It
  is intentionally empty for denials and provider-response decisions.
- Redaction summaries, source hashes, chunk hashes, and response hashes are
  evidence handles, not raw content.
- Provider responses are checked separately with `EvaluateResponse`; a passed
  preflight does not make model output trustworthy.
- This package is a contract package for #1756 and later provider work. It must
  stay free of storage, telemetry emitters, provider SDKs, and prompt builders.

## Related docs

- `docs/internal/design/1755-semantic-extraction-security-gates.md`
- `docs/internal/design/1758-documentation-semantic-observations.md`
- `docs/public/reference/telemetry/index.md`
