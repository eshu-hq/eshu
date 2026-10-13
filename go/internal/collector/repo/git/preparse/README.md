# Pre-Parse Identity

## Purpose

`preparse` decides, before the parse, whether a forced reconcile of a git
repository would emit the same facts as the scope's active generation (#7978
follow-up, ruling a9-143), and runs that decision for the git collector. When
it would, the collector skips the snapshot and the store re-projects the active
generation, which is what the ingestion store did after a full parse before
this package existed.

## Ownership boundary

It owns the pre-parse identity, the classification of every env key that can
change emitted facts, the skip decision with its valve, and the decision's
telemetry. It does not run git or read Postgres itself: the git collector
supplies the inputs and the tree check, and the ingestion store owns the
re-open, which `Run` reaches through the `Reprojector` interface.

## Exported surface

- `Config`, `BinaryStat`: every input outside the commit that changes emitted
  facts.
- `Identity`, `ConfigDigest`, `IdentityPrefix`: the identity and its config
  part (pure).
- `Decide`, `Input`, `ActiveGeneration`, `Outcome`, `Outcomes`: the skip
  decision and its closed set of 14 outcomes (pure).
- `ReasonIntervalElapsed`, `ReasonGraphDirty`: the two eligible reasons.
- `FoldedEnvKeys`, `NonOutputEnvKeys`: the env key classification (`keys.go`).
- `Run`, `Attempt`, `Recorder`, `Reprojector`: one forced reconcile end to
  end. `Run` calls `Decide`, calls the store on a skip, maps the store's answer,
  and records the telemetry below.

See `doc.go` for the contract.

## Dependencies

`identity.go`, `decision.go`, and `keys.go` use the standard library only.
`run.go` also uses `internal/scope` (the reconcile state and the re-projection
request), `internal/telemetry` (the counter, the outcome dimension, the
refresh-skip reasons), `pkg/log`, and OpenTelemetry metric and trace.

## Telemetry

`Run` emits, per forced reconcile:

- one increment of `eshu_dp_collector_reconcile_parse_skip_decisions_total`
  labeled `outcome` with a value `Outcomes` returns;
- the span event `git.reconcile.parse_skip_decision` on the caller's span, with
  `scope_id`, `eshu.reconcile.parse_skip_outcome`, and
  `eshu.reconcile.parse_skipped`;
- INFO `git_reconcile_parse_skipped` when the parse did not run (`skipped`,
  `reprojection_pending`), or WARN `git_reconcile_parse_skip_refused` for an
  identity mismatch, a fenced re-open, or a store error.

## Gotchas / invariants

- A missed input is a silent wrong skip. A new env key read by the git
  collector, discovery, or the parsers fails `TestEveryEnvKeyIsClassified`
  until it is added to `FoldedEnvKeys` (and folded into `Config`) or to
  `NonOutputEnvKeys` with a reason.
- Build one `Config` from the values the snapshotter uses and pass the same
  value to the snapshotter and the selectors, so the stamped identity and the
  compared identity cannot drift. Each binary's wiring guard test fails when a
  selector that can force a reconcile has no `Preparse` source.
- Any doubt parses: every outcome but `skipped` and `reprojection_pending`
  parses as before.
- The valve: `MaxSkips` 0 never skips; N parses at least every N intervals.

## Related docs

- `docs/public/reference/reconciliation-sweep.md`
- `docs/internal/evidence/7978-reconcile-no-duplicate-generation.md`
