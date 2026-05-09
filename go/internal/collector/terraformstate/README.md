# Terraform State Collector

## Purpose

`internal/collector/terraformstate` owns the first reader-stack primitives for
Terraform state. It opens exact state sources, parses state with a streaming
JSON decoder, redacts values before they cross the parser boundary, and emits
Terraform-state fact envelopes.

This package does not discover state objects, schedule collector runs, write
graph rows, persist raw state, or call cloud APIs directly. Discovery,
coordinator claims, reducer projection, and AWS SDK wiring belong to later
integration slices.

## Current Surface

- `StateSource` opens one exact Terraform state stream.
- `LocalStateSource` reads an operator-approved absolute file path only.
- `S3StateSource` wraps a caller-supplied read-only object client and sends an
  exact bucket/key request with optional `If-None-Match` and version metadata.
- `Parse` turns one state stream into redacted Terraform-state facts.
- `ParseOptions` carries scope, generation, source, fencing, and redaction
  context.

## Safety Rules

- Raw state bytes are only allowed in the source reader and parser window.
- Full S3 URLs and local paths are not emitted in facts; parser facts use a
  locator hash in payload and source references.
- Local state is never inferred from Git content. It must be configured as an
  exact operator-approved source.
- S3 reads are exact object reads. Prefix-only keys are rejected.
- S3 write capability is rejected at source construction.
- Redaction key material is mandatory before parsing.
- Unknown provider-schema scalar attributes are redacted. Unknown composite
  attributes are dropped and represented by warning facts.

## Next Slices

- AWS SDK adapter for `S3ObjectClient`.
- DynamoDB lock metadata read-only adapter.
- Bounded parser memory fixture for large state files.
- Fact emission integration with coordinator claim fencing.
- Telemetry for source open, parser stream, and fact batch emission.
