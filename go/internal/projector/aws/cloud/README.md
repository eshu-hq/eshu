# AWS Cloud Projector Intents

## Purpose

`internal/projector/aws/cloud` groups AWS cloud-resource intent families that
need an additional responsibility namespace.

## Ownership boundary

This directory is a namespace, not a runtime layer. Its leaf packages select
trigger facts and build reducer-intent values. Root projector orchestration and
reducer graph writes remain in their existing owners.

## Exported surface

None. Import the leaf package that owns the required intent family.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. Projector and reducer runtime signals retain their existing ownership.

## Child packages

- `image` builds the AWS Lambda-to-container-image materialization intent.

## Related docs

- `go/internal/projector/aws/README.md`
- `docs/internal/design/naming-remediation.md`
