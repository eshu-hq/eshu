# AGENTS: go/internal/totp

Scoped agent instructions for this directory. Root `AGENTS.md`/`CLAUDE.md`
and `docs/internal/agent-guide.md` still apply; this file adds package-local
rules only.

## Rules

- This package is a pure cryptographic primitive: standard library only, no
  database, HTTP, storage, or telemetry concerns. Keep it that way — callers
  (storage, query handlers) own persistence, sealing, and observability.
- Never add a code path that returns, logs, or persists a raw TOTP secret.
  `GenerateSecret` returns one; every other function either consumes a
  caller-supplied secret for one computation or returns no secret at all.
- Any change to `GenerateCode`'s truncation logic MUST keep
  `TestGenerateCode_RFC6238AppendixB_SHA1_8Digit` green — those are the
  literal RFC 6238 Appendix B vectors, not a fixture that can be adjusted to
  match new code.
- `digitModulus` is the closed set of supported digit counts (6-8). Do not
  widen it without also adding an RFC-traceable justification; TOTP
  authenticator apps universally expect 6 digits, which is
  `DefaultDigits`.
- Use `crypto/subtle.ConstantTimeCompare` for any code comparison. A
  `==` string comparison on a guessable 6-digit code is a timing side
  channel this package exists partly to avoid.

## Verification

```bash
cd go && go test ./internal/totp/... -count=1 -v
cd go && go vet ./internal/totp/...
gofumpt -l internal/totp/*.go
```
