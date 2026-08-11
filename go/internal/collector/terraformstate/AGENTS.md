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
- Local state sources must be exact operator-approved absolute files or
  approved Git-local candidates resolved from safe metadata. Do not treat
  discovered `.tfstate` metadata as permission to read the file.
- S3 state sources must name an exact bucket/key. Prefix-only keys are rejected.
- S3 source construction must reject write-capable configuration.
- Facts must not include full S3 URLs or local paths. Use locator hashes in
  payloads and source references.
- Redaction key material is mandatory before parsing.
- Unknown provider-schema scalar attributes are redacted. Unknown composites
  are dropped via `skipNested` and observed through the
  `eshu_dp_drift_schema_unknown_composite_total` counter wired by
  `CompositeCaptureRecorder` so operators can detect provider-schema drift.
- Schema-known composite attributes are captured through the streaming nested
  walker in `composite_walker.go`. The walker reuses the existing
  `json.Decoder` (no `json.Unmarshal` calls), classifies every scalar leaf
  through `RedactionRules.Classify`, and emits the nested-singleton-array
  shape the drift loader's flattener expects. Memory growth is bounded by
  schema depth; the 48 MB ceiling enforced by
  `TestParseStream_PeakMemoryGate_CompositeCapture` is non-negotiable.
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
- Treating local `.tfstate` as normal Git content or persisting its raw bytes.

## Identity join keys are exempt from SchemaUnknown (#5870)

- `identityJoinKeys` (`arn`, `id`, `self_link`) are preserved raw under an
  unknown provider schema. The criterion is "a key a downstream reader JOINS
  on", not "a key that looks like an identifier" — redacting one corrupts graph
  truth (the row leaves the join and its resource reports
  `orphaned_cloud_resource`) rather than protecting a value. Do NOT widen the
  set by intuition; follow the readers first.
- Use `classificationSchemaTrust` when classifying an attribute for emission and
  the BARE `schemaTrust` everywhere else. `redactsAnchor` depends on the bare
  one: the correlation-anchor guarantee is deliberately NOT part of the
  exemption.
- The exemption runs after `isHardSensitiveStateAttribute` and never applies to
  composites. Operator-declared sensitive keys outrank it automatically, because
  `redact.RuleSet.Classify` tests them before schema trust — do not re-check
  them in the trust seam.
- Any change here must keep the `provider_schema_not_covered` detector working.
  Without it a stale schema bundle is silent, because the exemption removes the
  false-orphan wave that used to reveal it.
