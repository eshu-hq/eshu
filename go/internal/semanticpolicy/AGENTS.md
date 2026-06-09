# AGENTS.md - internal/semanticpolicy guidance

This package evaluates semantic extraction policy before hosted provider work.
It must stay pure and fail closed.

## Read first

1. `README.md` - ownership boundary, exported surface, and invariants
2. `doc.go` - godoc package contract
3. `policy.go` - exported types, parser, evaluator, and status projection
4. `normalize.go` - policy validation and canonicalization
5. `match.go` - request matching and source selector behavior
6. `go/internal/semanticprofile/README.md` - provider profile boundary
7. `docs/internal/design/1758-documentation-semantic-observations.md` - semantic
   observation security and policy context

## Invariants

- Do not load credentials, instantiate provider clients, read source content, or
  construct prompts here.
- Keep policy deny-by-default. Unknown source classes, missing profile rows,
  stale or missing ACL state, disabled policy, and unallowlisted sources must
  return denied decisions.
- Keep reason codes low cardinality. Raw source paths, document titles, tenant
  names, provider request ids, credential handles, prompts, and responses do not
  belong in status, metrics, or errors.
- Do not widen retention without security review. Raw prompt or response
  retention is outside this package's current contract.
- Do not add storage or telemetry dependencies. Callers emit telemetry using the
  returned state and reason.

## Verification

Run `cd go && go test ./internal/semanticpolicy -count=1` after changes. Run
`scripts/test-verify-package-docs.sh` and `scripts/verify-package-docs.sh`
because this is a Go package under `go/internal`.
