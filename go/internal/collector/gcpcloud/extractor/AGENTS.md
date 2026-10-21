# AGENTS.md — internal/collector/gcpcloud/extractor guidance

## Read First

1. `README.md` — what this leaf owns and why it exists.
2. `doc.go` — the godoc contract: registry, shapes, guarantees.
3. `../AGENTS.md` — the gcpcloud source-fact contract and invariants this
   leaf serves.
4. `docs/public/reference/gcp-cloud-collector-contract.md` — the provider
   source, scope/generation, fact family, payload boundary, telemetry, and
   fixture contract.

## Invariants

- Standard library only. This leaf must never import the parent `gcpcloud`
  package or any extractor group: the parent and the groups both depend on
  this leaf, so any reverse edge is an import cycle.
- Registration is init-driven and fails loudly: blank asset type, nil
  extractor, or duplicate registration panics at startup. Never swallow a
  registration error or fall back to a default extractor.
- Extractors receive the raw CAI resource.data transiently and return only
  bounded, redaction-safe attributes, correlation anchors, and typed
  relationships. Never persist the data blob, and never return secrets,
  data-plane content, raw policy JSON, IPs, or response bodies.
- Metric labels stay bounded enums. Never label-leak resource ids, project
  ids, names, labels, IAM members, DNS names, image references, URLs, or
  credential names — this leaf emits no metrics itself, and helpers must not
  add any.
- Keep every file under 500 lines; split before the cap.

## Common Changes

- Add a shared helper here only when two or more extractor groups need it.
  A single group's helper lives in that group's package.
- When a new extractor group moves out of the `gcpcloud` root, point its
  files at this leaf and add the group package to the collector binary's
  imports for init side effects. The `gcpcloud/compat_extractor.go` aliases
  keep unmoved callers compiling; delete each alias once its last caller
  moves.
- Update `README.md`, `doc.go`, and this file when the exported surface or
  contract changes. Then run `scripts/verify-package-docs.sh`.
