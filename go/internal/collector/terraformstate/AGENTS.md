# AGENTS.md - internal/collector/terraformstate

Use `README.md` and `doc.go` for the package contract. This file keeps the
agent-only safety rules for state sources, parser redaction, and identity.

## Read First

1. `README.md`, `doc.go`, `source_local.go`, `source_s3.go`, `parser.go`,
   `identity.go`, and `config.go`.
2. `attributes.go`, `tags.go`, `composite_walker.go`, and `json_token.go`
   before changing parser memory or redaction behavior.
3. `go/internal/redact/README.md` before changing value handling.
4. `go/internal/terraformschema/README.md` before changing schema resolver
   behavior.

## Mandatory Invariants

- Raw Terraform state bytes stay inside `StateSource` readers and parser-local
  decoder windows. Do not persist or log unredacted state values.
- Local state sources must be exact operator-approved absolute files or
  approved Git-local candidates resolved from safe metadata.
- S3 state sources must name an exact bucket/key and reject write-capable or
  prefix-only configuration.
- Facts, logs, spans, metrics, admin status, and content storage must not carry
  full S3 URLs, local paths, raw locators, or raw state bytes.
- Redaction key material is mandatory before parsing.
- `LocatorHash` includes version identity. `ScopeLocatorHash` is
  version-agnostic and is the drift join key.
- Unknown provider-schema scalars are redacted. Unknown composites are dropped
  through `skipNested` and observed through `CompositeCaptureRecorder`.
- Schema-known composite capture uses streaming `json.Decoder` traversal; do
  not use full-payload `json.Unmarshal`.
- `tags` and `tags_all` are correlation evidence only. Scalar tag keys and
  values still follow redaction rules; non-scalar tag values emit warnings.

## Change Routing

- State-source changes require exact-source tests, safe-error tests, and
  cancellation/too-large/not-modified coverage where relevant.
- Parser field changes require tests proving raw values do not leak and memory
  stays bounded.
- AWS SDK wiring belongs behind `S3ObjectClient`; keep SDK types out of parser
  code.
- Telemetry for collector integration belongs in collector/runtime wiring, not
  in `redact`.

## Do Not Change Without Owner Approval And Proof

- Do not treat discovered `.tfstate` metadata as permission to read raw state.
- Do not import graph, reducer, query, or storage packages here.
- Do not replace streaming parser behavior with full-payload decoding.

## Required Proof

- Run `cd go && go test ./internal/collector/terraformstate -count=1`.
- Run memory/composite focused tests when parser capture changes.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/internal/collector/terraformstate --fail-on contradicted,missing_evidence` from `go/`.
