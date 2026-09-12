# Secrets/IAM — Agent Instructions

Scope: `go/internal/query/secrets/` (package `secrets`).

## Ownership

This leaf owns the secrets/IAM posture read routes (#6642 Part A):
`handler.go` (`Handler`, `Mount`, the identity-trust-chain route),
`handler_tracing.go` (the span seam), `authz.go` (the scoped-token grant
check), `grant_posture.go` (the graph-backed grant-posture store),
`posture_handlers.go` (the privilege-posture-observation, secret-access-path,
and posture-gap routes), `posture_stores.go` (their three Postgres stores),
`summary.go` (the posture-summary route and its Postgres store), and
`trust_chain.go` (the identity-trust-chain Postgres store), plus this
package's own tests, moved in verbatim from root -- see README.md's Move
evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `secrets_alias.go`, cycling. Reach
  root-only helpers through `querycontract` (profiles, envelopes, capability
  registration, HTTP helpers, ports, repository-access filtering) or
  `queryspan` (the shared handler-span seam); if neither has what you need,
  it does not belong here -- ask before adding a new shared home.
- The five `IAM*Capability` consts (`handler.go`, `posture_handlers.go`,
  `summary.go`) MUST stay byte-identical to their pre-move string values
  (`secrets_iam.identity_trust_chains.list`, etc.) -- they are registered
  capability ids in the OpenAPI spec and documented error contracts, and
  root's `contract_secrets_iam.go` init() (Part C, not touched by this move)
  reads them through the unexported aliases in `secrets_alias.go` to register
  the production capability matrix rows. They are exported at their
  declaration for exactly that reason: this package's own tests
  (`main_test.go`'s `TestMain`) and root's alias file are the callers that
  need the package-local name.
- `secretsHandlerTracer` is this package's own tracer var (the same seam
  `packageregTracer` in `go/internal/query/package/registry/handler_tracing.go`
  uses): package-local, seeded from `queryspan.HandlerTracer()`. Do not
  promote it to an exported var or move the span helper back to root.
- Every exported type, top-level func, and the five capability consts drop
  the leading `Secrets` word from their pre-move root spelling
  (`SecretsIAMHandler` -> `Handler`, `PostgresSecretsIAMPostureGapStore` ->
  `PostgresIAMPostureGapStore`, `NewGraphSecretsIAMGrantPostureStore` ->
  `NewGraphIAMGrantPostureStore`, and so on). Interface list methods keep
  their pre-move spelling (`ListSecretsIAMPostureGaps`,
  `SummarizeSecretsIAMPosture`): naming.md rule 4's no-stutter rule is about
  package-qualified identifiers, and a method is never written
  `secrets.ListSecretsIAMPostureGaps`, so there is nothing to drop. Root's
  `secrets_alias.go` keeps the pre-move spelling for every staying caller
  (cmd/api's and cmd/mcp-server's wiring, and the five capability consts)
  through a type alias and thin forwarders. Do not export anything else root
  does not need; new code should call this package's names directly rather
  than gaining a new root forward.
- Unexported SQL query text, Cypher fragments, fact-kind consts, and their
  const/field names (`secretsIAMPostureGapFactKind`,
  `secretsIAMPostureSummaryQueryTemplate`, `listSecretsIAM*Query`, etc.) keep
  their pre-move spelling: they carry no package-name stutter concern and
  several are asserted by name in tests that moved with this family
  (`bucketCounts`, `secretsIAMPostureGapFactKind`, and
  `secretsIAMPostureSummaryQueryTemplate` in `summary_test.go`).
- `TestMain` in `main_test.go` MUST keep registering this family's five
  capabilities with `querycontract.RegisterCapabilities` before the suite
  runs. `go test ./internal/query/secrets` never links root package query
  (this package cannot import it without an import cycle), so root's
  `contract_secrets_iam.go` init() never runs in this test binary; without
  `TestMain`, every handler test here would fail with the capability gate's
  `unsupported_capability` 501 for a reason unrelated to the handler under
  test. The support row it registers is copied faithfully from
  `contract_secrets_iam.go`'s init() (all five capabilities share the same
  local-authoritative-and-up exact ceiling) and must be kept in sync if that
  row ever changes (the `packagereg` TestMain precedent carries the identical
  constraint). Do not delete `main_test.go` as redundant.

## Naming

`docs/internal/naming.md` is law: no `secrets_iam_` file prefixes inside this
directory, no `secrets/secrets_iam.go`, exported identifiers lose the family
stutter except where the forwarder rule above names a specific root caller.
The root `secrets_alias.go` keeps every pre-move spelling that a caller
outside the family still uses (the `SecretsIAMHandler` alias, the five
capability consts, the six constructor forwarders) for staying callers; its
own filename still starts with `secrets_` (beside this `secrets/` directory)
because every alternative
spelling collides the same way -- it carries the
`//nolint:dirgate` package-line justification the `service_alias.go` and
`admin_alias.go` precedents use for the identical shape.
