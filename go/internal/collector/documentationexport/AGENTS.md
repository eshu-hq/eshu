# documentationexport Agent Notes

Read these before changing this package:

- `AGENTS.md`
- `docs/internal/agent-guide.md`
- `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
- `go/internal/collector/exportmanifestpreflight/README.md`
- `go/internal/collector/documentationexport/README.md`

## Invariants

- Keep the package default-off and parser-only.
- Run `exportmanifestpreflight.Preflight` before parsing file bytes.
- Return no facts when preflight reports any warning.
- Emit only documentation source, document, section, and link facts unless a
  later design explicitly approves mention or claim facts.
- Keep raw provider scope IDs, item IDs, private URLs, tokens, user identifiers,
  channel names, tenant identifiers, and credential-looking values out of
  durable metadata, logs, docs, fixtures, and PR text.
- Do not add live provider clients, runtime flags, Compose or Helm wiring, graph
  writes, queues, workers, goroutines, or credential handling here.

## Verification

Run at minimum:

```bash
cd go && go test ./internal/collector/documentationexport -count=1
scripts/verify-package-docs.sh
git diff --check
```

Run the collector authoring and performance evidence gates when behavior or
package docs change.
