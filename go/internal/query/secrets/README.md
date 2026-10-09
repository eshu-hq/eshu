# Secrets/IAM

## Purpose

The secrets/IAM posture read surface: five bounded, paginated, read-only
routes under `/api/v0/secrets-iam/*` over reducer-owned trust-chain, privilege
posture, secret access path, and posture gap facts, plus a scope-anchored
posture summary that blends those facts with the canonical
`GRANTS_ACCESS_TO` graph edges for the S3 external-principal grant section
(issue #5643).

## Ownership boundary

Owns `Handler`, its HTTP dispatch and route capabilities, the five
Postgres-backed stores (identity trust chains, privilege posture
observations, secret access paths, posture gaps, posture summary), and the
graph-backed grant-posture store. Does not own the capability registry,
envelope/profile contract, HTTP param/response helpers, or repository-access
filtering (`querycontract`), or the handler-span seam construction helper
(`tracing`) -- those are separate leaves this package calls into.

## Layout

- `handler.go` -- `Handler`, `Mount`, `listIdentityTrustChains`, the shared
  `profile`/`requiredSecretsIAMTrustChainLimit` helpers, and the identity
  trust-chain route's capability and result type.
- `handler_tracing.go` -- the route's own span seam (`secretsHandlerTracer`,
  `startQueryHandlerSpan`), the same shape the other leaves keep in their
  `handler_tracing.go`.
- `authz.go` -- `authorizeSecretsIAMScopedScope`, the scoped-token grant check
  shared by every route.
- `grant_posture.go` -- `IAMGrantPosture`, `GraphIAMGrantPostureStore`, its
  five bounded aggregate Cypher statements, and
  `NewGraphIAMGrantPostureStore`.
- `posture_handlers.go` -- the privilege-posture-observation,
  secret-access-path, and posture-gap route handlers, their capabilities, and
  result types.
- `posture_stores.go` -- the three corresponding Postgres-backed stores,
  filters, rows, and their SELECT templates.
- `summary.go` -- `IAMPostureSummary`, `IAMBucketCount`,
  `PostgresIAMPostureSummaryStore`, its GROUP BY template, and the
  posture-summary route handler (which blends in the grant-posture store's
  result when wired).
- `trust_chain.go` -- `IAMIdentityTrustChainStore`,
  `PostgresIAMIdentityTrustChainStore`, its filter, row, and SELECT template.
- Test files -- this package's own tests, moved in verbatim from root (see
  Move evidence below).
- `main_test.go` -- `TestMain`, which registers this family's five
  capabilities with `querycontract` before the suite runs (see AGENTS.md).

## Move evidence

The family moved here from the query root (`secrets_iam.go` and its
`secrets_iam_*.go` siblings, #6642 Part A); the non-test files are
destuttered (`secrets_iam.go` -> `handler.go`, `secrets_iam_authz.go` ->
`authz.go`, `secrets_iam_grant_posture.go` -> `grant_posture.go`,
`secrets_iam_posture_handlers.go` -> `posture_handlers.go`,
`secrets_iam_posture_stores.go` -> `posture_stores.go`, `secrets_iam_summary.go`
-> `summary.go`, `secrets_iam_trust_chain.go` -> `trust_chain.go`), and every
exported type, top-level func, and the five capability consts drop the
leading `Secrets` word (`SecretsIAMHandler` -> `Handler`,
`SecretsIAMPostureGapFilter` -> `IAMPostureGapFilter`,
`PostgresSecretsIAMPostureGapStore` -> `PostgresIAMPostureGapStore`,
`GraphSecretsIAMGrantPostureStore` -> `GraphIAMGrantPostureStore`,
`NewPostgresSecretsIAM...` -> `NewPostgresIAM...`, and so on), and every
family keeps the `IAM` word for consistency
(`IdentityTrustChain*` -> `IAMIdentityTrustChain*`,
`PrivilegePostureObservation*` -> `IAMPrivilegePostureObservation*`; the other
four families already carried it). Interface list methods keep their
pre-move spelling (`ListSecretsIAMPostureGaps`, `SummarizeSecretsIAMPosture`,
etc.): naming.md rule 4's no-stutter rule is about package-qualified
identifiers, and a method is never written `secrets.ListSecretsIAMPostureGaps`,
so there is no stutter to drop. Unexported constants, fields, and SQL/Cypher
identifiers keep their pre-move spelling too.
Root keeps every pre-move spelling that a caller outside the family still
uses through a stanza in the new `secrets_alias.go`: the `SecretsIAMHandler`
type alias, the five unexported capability consts, and the six Postgres/Graph
store constructor forwarders cmd/api's and cmd/mcp-server's wiring call.

Test disposition: `secrets_iam_grant_posture_test.go`, `secrets_iam_posture_test.go`,
and `secrets_iam_test.go` moved verbatim (destuttered to `grant_posture_test.go`,
`posture_test.go`, and `handler_test.go`). `secrets_iam_summary_test.go` split:
its one graph-read-sweep case (`TestSecretsIAMPostureSummaryGraphReadSweep`,
which drives the mounted route through the root alias types against the
root-shared `graphReadSweepCases`/`assertGraphReadSweepResponse` sentinel
table) stayed in root as `graph_read_error_secrets_iam_test.go`; every other
case in that file, including its `recordingPostureSummaryStore` and
`recordingGrantPostureStore` fixtures, moved here as `summary_test.go`.
`secrets_iam_authz_test.go` stayed in root unmodified: it calls root's
unexported `scopedHTTPRouteSupportsTenantFilter` (`auth_scoped_routes.go`)
directly, which this leaf cannot reach without an import cycle.
`secrets_iam_contract_test.go` stayed in root, renamed to
`capability_matrix_secrets_iam_test.go` (the `contract_` prefix is reserved
for Part C): it asserts root's `capabilityMatrix` rows through the alias
consts, with its own local recording-store stubs since the four family test
files that used to define them moved out.

No-Regression Evidence: baseline `origin/main` vs this branch -- `go build
./...`, `go vet ./internal/query/... ./cmd/api ./cmd/mcp-server ./internal/mcp`,
and `go test ./internal/query/...` and `go test ./internal/query/secrets/`
pass, and their combined test-name union equals the pre-move `go test
./internal/query/ -list '.*'` list exactly; the queryplan `source_sha256`
pins for `(GraphIAMGrantPostureStore).grantFlagCount` and
`(GraphIAMGrantPostureStore).groupedGrantCounts` are re-pinned in
`internal/queryplan/testdata/query-source-coverage.yaml` under the new
`secrets/grant_posture.go` path.

No-Observability-Change: every route keeps its span name
(`telemetry.SpanQuerySecretsIAM*`) and `http.route`/`eshu.capability`
attributes unchanged. `secretsHandlerTracer` is this package's own
package-local tracer var (mirroring `packageregTracer` in
`go/internal/query/package/registry/handler_tracing.go`), seeded from
`tracing.HandlerTracer()`.

## Failed reads (#7674)

Every route answers a failed read-model or grant-graph read with a fixed
message per step (for example `secrets/IAM posture gap query failed` or
`secrets/IAM S3 external-principal grant posture query failed`), never the
backend error text. `writeSecretsReadFailure` (`handler_tracing.go`) runs
`querycontract.WriteGraphReadError` first, so a stale or timed-out PostgreSQL
reader answers the retryable `503` with `Retry-After`; before #7674 only the
posture summary's grant section did, and the five read-model reads answered
500. Anything else answers `500` with the fixed message and records the error
on the handler span, or `499` with an `eshu.request.client_canceled` event and
no span error when the caller canceled the request. `server_failure_test.go`
covers each route and step with a canary fault, a client cancel, and a stale
reader.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No SQL, Cypher, query parameter, call count, row bound,
scope check, or success path changed. A failure now costs one span
`RecordError`/`SetStatus` and a fixed-string write instead of formatting the
error into the body. `go test ./internal/query/... ./internal/queryplan/...
-count=1` and `go test -race ./internal/query/secrets/...` exit 0. No
benchmark is claimed because the success path has no runtime delta to
measure.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the step's
fixed message as the description; a client cancel adds
`eshu.request.client_canceled`, leaves the status Unset, and answers `499`.
`server_failure_test.go` asserts both span shapes with a recording tracer for
every route and step.

## Related docs

- `go/internal/query/read-models.md`
- `docs/public/reference/source-layout.md`

All five Postgres-backed stores expose `WithReadStore` constructors for guarded query-only reads; legacy constructors remain available.
