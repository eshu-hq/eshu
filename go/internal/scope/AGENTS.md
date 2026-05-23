# internal/scope Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `scope.go` before changing scope or generation lifecycle types.
3. `tfstate.go` before changing Terraform-state scope identity.
4. `go/internal/storage/postgres/README.md` before changing persisted fields.

## Local Rules

- Keep this package pure value logic with standard-library imports only.
- Use `TransitionTo` or the named transition helpers; do not set
  `ScopeGeneration.Status` directly outside controlled construction.
- Keep terminal generation states terminal. `completed`, `failed`, and
  `superseded` must not gain casual outgoing transitions.
- Use `PreviousGenerationExists` for prior-generation checks. Do not infer prior
  history from `ActiveGenerationID`.
- Preserve timestamp validation: `IngestedAt` must not precede `ObservedAt`.
- Preserve required identifiers: scope ID, source system, scope kind, collector
  kind, partition key, generation ID, and trigger kind must stay non-blank.
- Terraform-state scope identity is backend kind plus locator hash. Serial and
  lineage belong to generation identity, not scope identity.
- Keep the Terraform-state locator hash byte-compatible with
  `terraformstate.ScopeLocatorHash`.

## Change Gates

- New status values require transition-table updates, README lifecycle updates,
  and tests for valid and invalid transitions.
- New persisted fields require additive validation, Postgres storage support,
  and migration/default behavior in the same PR.
- New scope or collector kind constants require downstream switch checks in
  collectors and Postgres storage.

## Do Not Change Without Owner Review

- Stored string values for scope kind, collector kind, trigger kind, or
  generation status.
- `allowedGenerationTransitions`.
- Terraform-state scope and generation identity formulas.
