# semanticqueue Agent Notes

This package plans semantic extraction queue records. Keep it side-effect free:
no provider calls, no database access, no prompt construction, no raw prompt or
response retention, and no graph admission.

Every lifecycle change must keep job identity deterministic and retry safe.
When adding fields, prefer low-cardinality enums and hashes suitable for status,
audit, and telemetry aggregation. Do not add raw source paths, URLs, display
titles, user names, tenant names, credential handles, prompts, responses, or
provider error bodies.

Run focused tests with:

```bash
cd go && go test ./internal/semanticqueue -count=1
```
