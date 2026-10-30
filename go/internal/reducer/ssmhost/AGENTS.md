# AGENTS.md — internal/reducer/ssmhost

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` still applies; these add to it.

## Import rule

This package is a reducer family. It may import `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `internal/facts`,
`internal/facts/cloud`, `internal/telemetry`, and `pkg/log`. It must
**never** import the parent `internal/reducer` package or any sibling family
package, directly or transitively. The query read model imports this package
for decode and assessment; keep the imported surface pure (no driver, no
queue, no clock I/O outside the injected `Now`).

## What must stay conservative

- The handler MUST validate `Bounds` before loading facts. Zero bounds fail
  closed; unbounded freshness rules must never govern supersession.
- The writer MUST merge nodes and edges by stable uid and MUST scope retracts
  to one host plus the evidence source. A scope-wide retract would delete a
  silent host's preserved truth.
- The extractor MUST NEVER emit vulnerability, supply-chain, PURL, or image
  digest content. New row fields need an allowlist reason in `README.md`.
- A malformed fact is quarantined per-fact (`input_invalid`) while every
  valid fact in the same batch still projects. Schema-major skew is fatal to
  the whole intent, never quarantined.
- Cross-generation interleaving relies on the runtime's generation
  supersession short-circuit; within one generation every write and retract
  is uid-idempotent. Do not add locking to the handler.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`.
- **`verify-telemetry-coverage.sh`** — every new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  package adds no instruments; new files carry the honest
  no-new-instrument row.
- **`verify-doc-citations.sh`** — never write a `file.go:NNN` pattern in a
  comment, README, or AGENTS.md. Debt may only decrease.
