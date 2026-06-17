# AGENTS.md — cmd/audit-report guidance for LLM assistants

## Read first

1. `go/cmd/audit-report/README.md` — usage, flags, fixtures.
2. `go/cmd/audit-report/main.go` — `run`, `loadIssues`; logic lives in
   `internal/auditreport`.
3. `go/internal/auditreport/README.md` — the generator contract.

## Invariants this package enforces

- **Thin driver.** `main` calls `run`; `run` loads input, the embedded catalog,
  and optional issues, then delegates to `auditreport.Generate` and a renderer.
- **No issue creation.** The command only reports. Do not add GitHub writes.
- **Offline and deterministic.** Duplicate detection reads an issues file, not
  the network; the Markdown report is golden-tested.

## Common changes and how to scope them

- **New format** → add a case to the format switch; add a test.
- **Catalog source change** → keep using `capabilitycatalog.Load` (embedded) so
  the command stays offline.

## Failure modes and how to debug

- Symptom: golden mismatch → regenerate with
  `go test ./cmd/audit-report -run Golden -update` after confirming the change is
  intended.

## What NOT to change without an ADR

- The no-issue-creation and offline contracts.
