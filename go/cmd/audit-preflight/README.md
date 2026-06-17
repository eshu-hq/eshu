# cmd/audit-preflight

`audit-preflight` is the competitive-audit issue gate. It validates that an audit
issue states the required evidence and a gap classification before it becomes
work. All logic lives in `go/internal/auditpreflight`; this binary is a thin
file/stdin driver.

## Usage

```bash
cd go
# Validate a saved issue body.
go run ./cmd/audit-preflight -file path/to/issue.md
# Pipe an existing issue.
gh issue view <N> --json body -q .body | go run ./cmd/audit-preflight
```

Exit code is non-zero when the issue fails the preflight contract, so it can gate
issue creation in automation.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-file` | stdin | path to the issue body file |

## Dogfood fixtures

`testdata/` holds the three competitor examples (graphify, GitNexus,
CodeGraphContext) plus an incomplete case, exercised by `main_test.go`.

## Related

- `go/internal/auditpreflight/README.md`
- [Competitive Audit Preflight](../../../docs/public/reference/competitive-audit-preflight.md)
