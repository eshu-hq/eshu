# AGENTS.md - internal/componentindex guidance for LLM assistants

## Read first

1. `go/internal/componentindex/README.md` - package purpose and invariants
2. `go/internal/componentindex/verify.go` - index schema and verifier
3. `docs/internal/design/1830-community-extension-index-publication-workflow.md`
4. `docs/public/reference/component-package-manager.md`
5. `docs/public/reference/plugin-trust-model.md`
6. `docs/public/reference/fact-schema-versioning.md`

## Invariants this package enforces

- Index membership is advisory. It never bypasses local component trust policy.
- Validation is offline and deterministic. Do not add network calls, registry
  pulls, GitHub topic discovery, or live provenance checks here.
- Revocation wins over installable state.
- Duplicate fact-kind claims are rejected until an explicit shared ownership
  contract exists.

## Common changes and how to scope them

- **Add an index field** - update `Entry`, `Validate`, focused tests, and the
  package README.
- **Add a failure class** - add a stable `IssueCode` and table-driven coverage.
- **Add registry or publication behavior** - keep it outside this package unless
  an accepted design explicitly expands the ownership boundary.

## Anti-patterns specific to this package

- Do not treat index entries as trusted components.
- Do not make GitHub topics or search results authoritative.
- Do not perform network I/O in validation.
- Do not weaken local disabled, allowlist, strict, revocation, or compatible
  core gates.
