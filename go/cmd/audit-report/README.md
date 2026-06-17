# cmd/audit-report

`audit-report` generates a deterministic, offline competitive-audit report from a
declarative audit input, reconciled against the embedded capability catalog and
an optional open-issues list. All logic lives in `go/internal/auditreport`; this
binary is a thin driver. It never creates issues.

## Usage

```bash
cd go
# Markdown report.
go run ./cmd/audit-report -input audit.yaml -format md
# With duplicate detection against open issues (offline, deterministic).
gh issue list --json number,title --limit 200 > issues.json
go run ./cmd/audit-report -input audit.yaml -issues issues.json -format json
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-input` | (required) | audit input YAML |
| `-issues` | none | open issues JSON from `gh issue list --json number,title` |
| `-format` | `md` | `md` or `json` |

## Input shape

See `testdata/audit-input.yaml`. Each finding records the competitor source files
inspected, Eshu evidence files, an optional `eshu_capability`, and a proposed
gap class and owner surface.

## Fixtures

`testdata/` holds the graphify/GitNexus/CodeGraphContext dogfood input, an open
issues fixture, and the golden Markdown report (`main_test.go`, refresh with
`-update`).

The golden test runs the real command, which loads the embedded capability
catalog. The fixture references existing catalog capabilities, so a catalog
regeneration that renames a referenced capability or changes its linked issues
will change the report. After any catalog change, refresh the golden:

```bash
cd go && go test ./cmd/audit-report -run TestRunMarkdownMatchesGolden -update
```

## Related

- `go/internal/auditreport/README.md`
- [Competitive Audit Preflight](../../../docs/public/reference/competitive-audit-preflight.md)
