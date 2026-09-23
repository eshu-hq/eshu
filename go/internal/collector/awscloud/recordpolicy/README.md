# recordpolicy

The AWS collector's record-mode field table for `#6965` Phase 3: which
pseudonym shape each `aws/v1` payload key takes when `collector-aws-cloud
-mode=record` writes a cassette.

## Purpose

`Policy()` returns a `recordpseudo.Policy` mapping every payload key the AWS
fact schemas declare (`sdk/go/factschema/schema/aws_*.json`), the scope
metadata keys, and the attribute keys of the services in the committed corpus
to a `recordpseudo.Class`. The engine in `go/internal/replay/recordpseudo`
is collector-neutral; this table is what makes it AWS-aware.

## Ownership boundary

This package owns the classification only. It does not pseudonymize
(recordpseudo does), does not build envelopes (`awscloud` does), and is not
imported by any live path: only the record-mode wiring in
`cmd/collector-aws-cloud` and tests reach it.

## Exported surface

- `Policy() recordpseudo.Policy` -- a fresh copy of the table on every call.

## Fail-closed contract

A key that is not in the table is unclassified: its string values are made
opaque and its path is reported in the record log event. The one corpus key
deliberately left out is `attributes.containers[].runtime_id`. The package
test walks the checked-in AWS JSON schemas and fails on any schema key the
table does not list, so a new contract field cannot land unclassified.

## Class choices worth knowing

- `account_id` and friends are `ClassAccount`: 12 digits stay 12 digits
  (`0000` + 8) because four replay-path validators require that shape.
- `resource_id`, `source_resource_id`, `target_resource_id`, `source_value`,
  `values` and `principal_value` are `ClassIdent`: their shape is sniffed
  (ARN, ECR reference, AWS-issued id, CIDR, address, hostname, name).
- `environment`, `workload_id`'s `workload:` prefix, enums, digests and
  timestamps are kept, so `USES` materialization and image identity survive.
- `description`, `message` and `unsupported_key` are opaque: free text.

## Telemetry

None of its own. The record log event `collector.record.pseudonymized`
(emitted by `cmd/collector-aws-cloud`) reports the opaque and unclassified
paths this table produced.

## Validation

```bash
cd go && go test ./internal/collector/awscloud/recordpolicy -count=1
```
