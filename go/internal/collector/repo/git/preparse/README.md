# Pre-Parse Identity

## Purpose

`preparse` decides, before the parse, whether a forced reconcile of a git
repository would emit the same facts as the scope's active generation (#7978
follow-up, ruling a9-143). When it would, the git collector skips the snapshot
and re-projects the active generation, which is what the ingestion store did
after a full parse before this package existed.

## Ownership boundary

It owns the pre-parse identity, the classification of every env key that can
change emitted facts, and the skip decision with its valve. It does not run
git, read Postgres, or re-project anything: the git collector supplies the
inputs and runs the tree check, and the ingestion store owns the re-open.

## Exported surface

- `Config`, `BinaryStat` — every input outside the commit that changes emitted
  facts.
- `Identity`, `ConfigDigest`, `IdentityPrefix` — the identity and its config
  part.
- `Decide`, `Input`, `ActiveGeneration`, `Outcome`, `Outcomes` — the skip
  decision and its closed outcome set.
- `ReasonIntervalElapsed`, `ReasonGraphDirty` — the two eligible reasons.
- `FoldedEnvKeys`, `NonOutputEnvKeys` — the env key classification.

See `doc.go` for the contract.

## Dependencies

Standard library only.

## Telemetry

None here. The git collector counts each decision on
`eshu_dp_collector_reconcile_parse_skip_decisions_total{outcome}` with the
values `Outcomes` returns.

## Gotchas / invariants

- A missed input is a silent wrong skip. A new env key read by the git
  collector, discovery, or the parsers fails `TestEveryEnvKeyIsClassified`
  until it is added to `FoldedEnvKeys` (and folded into `Config`) or to
  `NonOutputEnvKeys` with a reason.
- Build one `Config` from the values the snapshotter uses and pass the same
  value to the snapshotter and the selectors, so the stamped identity and the
  compared identity cannot drift.
- Any doubt parses: every outcome but `skipped` parses as before.
- The valve: `MaxSkips` 0 never skips; N parses at least every N intervals.

## Related docs

- `docs/public/reference/reconciliation-sweep.md`
- `docs/internal/evidence/7978-reconcile-no-duplicate-generation.md`
