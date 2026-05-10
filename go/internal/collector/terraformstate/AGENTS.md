# AGENTS.md — internal/collector/terraformstate guidance for LLM assistants

## Read first

1. `go/internal/collector/terraformstate/README.md` — package contract,
   safety rules, and current surface
2. `go/internal/collector/terraformstate/source_local.go` — explicit local
   source behavior
3. `go/internal/collector/terraformstate/source_s3.go` — read-only S3 source
   seam and exact-key validation
4. `go/internal/collector/terraformstate/parser.go` — streaming parser,
   redaction, warning facts, and envelope construction
5. `go/internal/redact/README.md` — redaction invariants before changing value
   handling

## Invariants this package enforces

- Raw Terraform state bytes stay inside source readers and parser-local JSON
  decoder windows.
- Local state sources must be exact operator-approved absolute files. Do not
  infer local `.tfstate` from Git repository discovery.
- S3 state sources must name an exact bucket/key. Prefix-only keys are rejected.
- S3 source construction must reject write-capable configuration.
- Facts must not include full S3 URLs or local paths. Use locator hashes in
  payloads and source references.
- Redaction key material is mandatory before parsing.
- Unknown provider-schema scalar attributes are redacted. Unknown composites are
  dropped and represented by warning facts.
- `tags` and `tags_all` are emitted as correlation evidence, but scalar tag
  keys and values still follow the unknown provider-schema rule and are
  redacted by default. Non-scalar tag values are dropped with warning facts.

## Common changes and how to scope them

- Add AWS SDK wiring behind `S3ObjectClient`; keep SDK types out of parser code.
- Add DynamoDB lock metadata behind a small read-only interface.
- Add parser fields through tests that prove raw values do not leak.
- Add telemetry in collector-owned integration code, not inside `redact`.

## Anti-patterns specific to this package

- Calling `json.Unmarshal` on the full Terraform state payload.
- Persisting raw state bytes or full source locators in facts, logs, spans,
  metrics, admin status, or content storage.
- Adding graph, reducer, query, or storage imports to this package.
- Treating local `.tfstate` as normal Git content.
