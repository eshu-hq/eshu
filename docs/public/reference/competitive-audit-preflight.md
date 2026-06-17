# Competitive Audit Preflight

Competitive-audit issues are easy to over-create: the reviewer must reconcile
competitor code, Eshu code, docs, tests, and existing issues by hand, which has
produced avoidable misses when Eshu had foundation work merged but not surfaced.
The preflight makes every audit issue state what was validated before it becomes
work, using the [capability catalog](capability-catalog.md) as the comparison
point.

## Required evidence

Every competitive-audit issue must fill these sections (the
[issue form](https://github.com/eshu-hq/eshu/issues/new?template=competitive-audit.yml)
collects them and the validator checks them):

| Section | What it captures |
| --- | --- |
| Competitor source and local path | Competitor name, local repo path, and the source file/feature observed — not docs-only claims. |
| Eshu code evidence | Eshu code paths inspected and what they already do. |
| Eshu docs evidence | Eshu docs inspected, including the capability catalog. |
| Eshu test or proof evidence | Tests, contracts, or proof runs inspected. |
| Existing issue duplicate search | Search terms against open issues and recent closed PRs/issues, and results. |
| Gap class | One of: `missing`, `foundation exists`, `ui missing`, `docs stale`, `proof missing`, `quality gap`, `already tracked`. |
| Owner surface | One of: `api`, `mcp`, `console`, `docs`, `collector`, `parser`, `reducer`, `correlation`, `search`, `runtime`, `governance`. |
| Verification plan | How closing the issue is proven, or why it links to an existing issue. |

## Validator

```bash
cd go
# Validate a saved issue body.
go run ./cmd/audit-preflight -file path/to/issue.md
# Or pipe it in.
gh issue view <N> --json body -q .body | go run ./cmd/audit-preflight
```

The validator fails when a required section is absent or empty, or when the gap
class or owner surface is outside the taxonomy. The gap-class and owner-surface
taxonomy is owned by `go/internal/auditpreflight` and reused by the local
[competitive audit report generator](#related).

## Duplicate detection

Before creating an issue, search both open and recently closed work:

```bash
gh issue list --state open --search "<terms>"
gh issue list --state closed --limit 50 --search "<terms>"
gh pr list --state merged --limit 50 --search "<terms>"
```

Keep domain work linked to the existing epics (for example #2676 hybrid
retrieval, #2711 capability catalog) instead of creating duplicates. If the gap
is `already tracked`, link the existing issue and do not open a new one.

## Dogfood examples

The three classic comparisons map to three outcomes (validated fixtures live in
`go/cmd/audit-preflight/testdata`):

| Competitor | Gap class | Outcome |
| --- | --- | --- |
| graphify (symbol graph) | foundation exists | No new issue; the foundation ships as `code_search.symbol_lookup`. |
| GitNexus (commit timeline) | missing | Create a new child issue with a verification plan. |
| CodeGraphContext (semantic retrieval) | already tracked | Reject as duplicate; link epic #2676. |

## Local audit report generator

For repeated audits, drive the preflight from a declarative input instead of
hand-writing each issue. The local report generator reconciles an audit input
against the capability catalog and an optional open-issues list, classifies each
finding with this taxonomy, and recommends an action — no issue, link existing,
update existing, draft new, or review. It never creates issues and runs offline.

```bash
cd go
gh issue list --json number,title --limit 200 > issues.json   # optional, for duplicate detection
go run ./cmd/audit-report -input audit.yaml -issues issues.json -format md
```

The input lists competitors and per-feature findings with the competitor source
files inspected, Eshu evidence files, an optional `eshu_capability`, and a
proposed gap class and owner surface. See `go/cmd/audit-report/testdata` for the
graphify/GitNexus/CodeGraphContext dogfood example and its golden report.

## Related

- [Capability Catalog](capability-catalog.md)
- `go/internal/auditpreflight/README.md`
- `go/internal/auditreport/README.md`
