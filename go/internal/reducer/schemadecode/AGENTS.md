# Agent instructions: internal/reducer/schemadecode

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Decode seams only: one function per fact kind, turning a stored payload into a
typed value or classifying it as a terminal dead letter. No storage reads, no
graph writes, no queue work, no handler policy.

## Filenames are a contract — read this before moving or renaming anything

The payload-usage manifest gate resolves decode seams by the
`factschema_decode*.go` basename, and since #6055 it searches one directory below
the reducer root as well as the root itself. `filepath.Glob` never crosses a `/`,
which is why that fix exists.

So: a seam file keeps its `factschema_decode_*.go` basename when it lives here.
Do not "tidy" the stutter away by renaming `factschema_decode_azure.go` to
`azure.go`. The gate resolves by basename, and a renamed file drops its fact kinds
out of the manifest quietly.

For the same reason the root compatibility files are named `decode_seam_compat*.go`,
not `factschema_decode_compat*.go` — the latter matched the seam glob while
containing only forwarders, and the gate failed with "no decode seams found".

## Import budget

The per-domain `sdk/go/factschema/*` packages, plus `internal/reducer/factdecode`
for `QuarantinedFact` and `NewFactDecodeError`. Nothing from the reducer root, no
family package, no storage package.

The domain-schema imports are the whole reason this package is separate from
`factdecode`. Do not move these seams into `factdecode` to reduce the package
count — that package's import budget exists to keep domain schema packages out of
the mechanism tier.

## Adding a decoder

Add it to the file for its domain, keeping the `factschema_decode_<domain>.go`
basename. Export it, and add an unexported forwarder of the lowercase spelling to
a root `decode_seam_compat*.go` file only if a reducer-root call site needs it —
a family package should import this package directly.

Add a row to `docs/public/observability/telemetry-coverage.md`. These files emit
no signal of their own, so use the `No-Observability-Change:` marker and name the
signals that do cover the path: `eshu_dp_reducer_input_invalid_facts_total` for a
malformed field, and `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds` for the owning pass. Verify any metric name
on its definition line in `go/internal/telemetry/instruments.go` before writing
it — a name copied between docs can stay self-consistent while matching nothing
that is emitted.

## Decode failure is never fatal

A malformed payload is quarantined and the pass continues. Do not add a decoder
that returns a fatal error or panics on bad input.
