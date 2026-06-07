# AGENTS.md - internal/workflowimage guidance for LLM assistants

## Read first

1. `README.md` - package purpose, ownership, and redaction boundary.
2. `doc.go` - godoc package contract.
3. `extract.go` - GitHub Actions workflow command parsing and image evidence classification.

## Invariants

- Extractors return public-safe metadata only. Do not return raw shell commands,
  environment values, tokens, URLs with credentials, or workflow output values.
- Exact image evidence requires an explicit normalized image ref. Repository or
  service name coincidence is not evidence.
- Templated expressions, shell variables, and matrix values stay unresolved.
- Multiple image refs in one command stay ambiguous unless a caller explicitly
  owns a stronger disambiguation model.
- This package does not emit facts, write graph truth, or read storage.

## Verification

Run focused tests after changes:

```bash
cd go && go test ./internal/workflowimage -count=1
```
