# internal/correlation/explain Agent Rules

This package owns only the stable, line-oriented explain text format. It MUST
NOT evaluate rules, apply admission, select winners, mutate candidates, or add
rejection reasons.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `explain.go` and `explain_test.go`.
3. `../engine/README.md` and `../model/README.md`.
4. Any API or MCP caller that parses or returns explain output before changing
   the format.

## Local Invariants

- Section order MUST remain header, sorted match counts, sorted rejection
  reasons, sorted evidence.
- Match-count and rejection-reason lines MUST be sorted before rendering.
- Evidence MUST be cloned before sorting; do not mutate the input result.
- Evidence sort order is `(ID, SourceSystem, EvidenceType)`.
- Confidence rendering MUST stay `%.2f`.
- Output MUST NOT include a trailing newline.
- Evidence values are rendered verbatim. Escaping or quoting is a wire-format
  change and belongs at the API boundary only with compatibility proof.

## Change Rules

- Any header, evidence-line, precision, section-order, or newline change is an
  output contract change. Update golden tests and every consumer that parses the
  format.
- New line types MUST have deterministic ordering and package docs.
- Do not call `engine.Evaluate` or `admission.Evaluate` from render code.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/explain -count=1
go vet ./internal/correlation/explain
go doc ./internal/correlation/explain
```

Format changes also need API/MCP compatibility proof. Docs-only edits also need
the package-doc verifier for this directory and `git diff --check`.
