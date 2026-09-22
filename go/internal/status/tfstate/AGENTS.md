# Agent instructions: internal/status/tfstate

Scope: `go/internal/status/tfstate` only. The parent package's instructions
in `go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. The root aggregates the family leaves into `RawSnapshot`/`Report`, so
root and leaves both import `tfstate`; an import in the other direction is
an import cycle. If you find yourself needing a type from the root here, the
type is in the wrong place — move it down, do not reach up.

Keep the dependency set to the standard library, `internal/status/shared`,
and `internal/tfstatewarning`. A new third-party or internal import in this
package is a design change, not a detail.

## Exporting the JSON entry point

`terraformStateReportJSON` is called directly from root `json.go` as
`terraformStateReportJSON(report.TerraformState)` and is unexported today.
Export it as `ReportJSON` on the move, and keep its nil-on-empty behavior —
it must return `nil` when `Report` carries no serials, warnings, or summary
rows, or the admin status response will start emitting an empty tfstate
section for every runtime that never observes tfstate evidence.

## Changing the wire helpers

`ReportJSON` and the row-level JSON projections render operator-facing
status JSON consumed by the status HTTP surfaces and MCP status tools. Their
struct tags and time formatting are the published contract.

Before changing any of them, know that they are locked by byte-for-byte
goldens: `internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, and the explicit dotted-key-path list in
`render_json_key_paths_golden.txt`. A failure there is a real API break to
justify, not a golden to regenerate reflexively.

## Safe-locator discipline

Every exported row type is keyed by `SafeLocatorHash`, never a raw bucket
name, S3 key, or local file path. Do not add a field that carries a raw
locator — the whole point of this family is that a Terraform-state warning
or serial can be reported without exposing where the state file lives.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens that prove this leaf's wire contract live in the parent package's
tests.
