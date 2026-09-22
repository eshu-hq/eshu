# Telemetry Contract

## Purpose

`contract` holds the per-family span name, metric-dimension, and log-key
declarations for Eshu's Go data-plane telemetry contract: admission
decisions, Azure relationships, bootstrap ingestion, CI/CD, collector runs
and snapshot stages, Neo4j graph-read outcomes, Kubernetes, language and
language-query, package registry, prompt-facing query spans, S3
external-principal grants, scanner-worker, security alerts, semantic
extraction, service catalog, source-tool provenance, supply-chain and
vulnerability findings, vulnerability-intelligence, incident context,
observability coverage, secrets/IAM, work-item evidence, generation
lifecycle, and changed-since reads. It replaces the 27 flat `contract_*.go`
files that used to live directly in `go/internal/telemetry` (issue #6777).

## Ownership boundary

Declarations only: string constants, bounded label-value constants, and the
handful of `Attr*` helper functions and exported vocabularies each family
needs. This package owns none of the *ordering* that used to live in each
file's `func init()` — that explicit, load-bearing registration order lives
in root `go/internal/telemetry/registration.go` and `registration_steps.go`.
`contract` has no `func init()` and never mutates a package-level slice.

`contract` must never import root package `telemetry` (root imports this
package for registration, so the reverse would cycle) or any other
`go/internal/*` package — it stays a leaf, same as root telemetry.

## Exported surface

Every exported constant, bounded value, and `Attr*`/vocabulary function
declared under the old `contract_*.go` files, now grouped one Go file per
family (see `doc.go` and each file's own doc comment for the full inventory).
Notable non-constant exports: `AttrLanguage`, `AttrStage`, `AttrSourceTool`,
and `VulnerabilitySuppressionMutationOutcomes`.

## Dependencies

- `go.opentelemetry.io/otel/attribute` — the `Attr*` helper return type.

No `go/internal/*` imports; see Ownership boundary.

## Telemetry

This package is telemetry — see Purpose. It does not itself emit metrics,
spans, or logs.

## Gotchas / invariants

- Root `go/internal/telemetry` re-exports every identifier here as a compat
  alias (`compat_contract.go`) so every existing external caller keeps
  compiling unchanged. Do not delete a stanza there while an external caller
  still uses the bare root name.
- File names keep their pre-move `z_`, `zz_`, `zzz_`, `zzzz_` prefixes for
  history; they no longer control registration order.
- `TestSpanNames`, `TestMetricDimensionKeys`, and `TestLogKeys` in root
  `contract_test.go` pin the exact frozen order these declarations are
  spliced into. Adding a new span, dimension, or log key here requires a new
  registration step in root `registration.go`/`registration_steps.go`, not an
  edit to those tests' expected lists.

## Related docs

- `go/internal/telemetry/README.md` — the parent package's full contract and
  layout.
- `docs/public/reference/telemetry/index.md`
