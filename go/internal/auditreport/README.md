# auditreport

`auditreport` generates a deterministic, offline competitive-audit report. It
reconciles an operator-authored audit input against the capability catalog and
an optional open-issues list, then recommends an action per finding. It never
creates issues and never scrapes competitor repositories.

## Input

A YAML spec (`AuditInput`) of competitors and per-feature findings. Each finding
records the competitor source files inspected, the Eshu evidence files, an
optional `eshu_capability` to reconcile against, and a proposed gap class and
owner surface (the `auditpreflight` taxonomy).

## Reconciliation and recommendation

`Generate(input, catalog, issues)` returns a `Report` sorted by competitor then
feature. Per finding it:

- validates the gap class and owner surface against the shared taxonomy;
- resolves `eshu_capability` against the catalog (maturity, linked issues);
- detects duplicate open issues by significant-token title overlap;
- recommends one of `no_issue`, `link_existing_issue`, `update_existing_issue`,
  `create_new_issue_draft`, or `review`.

Conflicts are surfaced, not hidden: a finding marked `missing` whose capability
exists in the catalog becomes `review`, and an invalid classification becomes
`review` with the validation findings attached.

## Output

`RenderMarkdown` (a summary plus a per-finding table) and `RenderJSON` (the full
`Report`) are deterministic. The command `go/cmd/audit-report` drives them.

## Related

- `go/internal/auditpreflight/README.md` — the shared taxonomy.
- `go/internal/capabilitycatalog/README.md` — the catalog source.
- [Competitive Audit Preflight](../../../docs/public/reference/competitive-audit-preflight.md)
