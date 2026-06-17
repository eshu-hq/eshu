# auditpreflight

`auditpreflight` validates competitive-audit issues against the Eshu preflight
contract so every audit issue states what was validated before it becomes work.
It owns the gap-class and owner-surface taxonomy shared by the issue gate
(`go/cmd/audit-preflight`) and the local competitive audit report generator
(issue #2716).

## Contract

A competitive-audit issue must fill these `### Heading` sections:

- Competitor source and local path
- Eshu code evidence
- Eshu docs evidence
- Eshu test or proof evidence
- Existing issue duplicate search
- Gap class — one of `GapClasses`
- Owner surface — one of `OwnerSurfaces`
- Verification plan

These headings match the GitHub issue form
`.github/ISSUE_TEMPLATE/competitive-audit.yml`; keep them in lockstep.

## API

- `ParseIssue(body) map[string]string` — sections keyed by lowercased heading,
  with `_No response_` normalized to empty.
- `Validate(body) []Finding` — `missing_field`, `empty_field`,
  `invalid_gap_class`, and `invalid_owner_surface` findings, sorted
  deterministically. An empty slice means the issue is preflight-complete.
- `GapClasses`, `OwnerSurfaces` — the closed taxonomy.

## Determinism

`Validate` matches gap class and owner surface after lowercasing and collapsing
whitespace, and sorts findings by kind then field, so output is stable.

## Related

- [Competitive Audit Preflight](../../../docs/public/reference/competitive-audit-preflight.md)
- `go/cmd/audit-preflight/README.md`
