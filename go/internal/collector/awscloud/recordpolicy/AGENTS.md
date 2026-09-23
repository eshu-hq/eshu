# AGENTS.md - internal/collector/awscloud/recordpolicy guidance

## Read First

1. `README.md` - what the table is for and the fail-closed contract.
2. `policy.go` - the key lists per class.
3. `go/internal/replay/recordpseudo/README.md` - the engine's class shapes.
4. `sdk/go/factschema/schema/aws_*.json` - the keys the table must cover.

## Invariants

- Every key an `aws/v1` JSON schema declares is listed here; the package test
  fails otherwise. Classify a new contract field in the same change that adds
  it to `sdk/go/factschema`.
- Never widen a class to make a recording "look nicer". A key whose consumer
  parses a shape (12-digit account, ARN grammar, `name:revision`) keeps the
  class that preserves that shape; a key nobody parses may be opaque.
- `attributes.containers[].runtime_id` stays unlisted on purpose; its path
  is expected in the record report.
- Do not import this package from a live-collection path. Record mode is the
  only caller.
- No real organisation, account or hostname literal in this package or its
  tests.

## Skill routing

- `golang-engineering` for Go changes.
- `eshu-golden-corpus-rigor` when a class change alters what a recorded
  cassette carries.
- `eshu-contract-rigor` when the covered key set changes with a schema.
