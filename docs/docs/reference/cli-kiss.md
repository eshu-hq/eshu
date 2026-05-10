# CLI K.I.S.S.

This page is the simple version.

If you only remember five things about `eshu`, make it these:

1. `eshu` has a local mode and a remote mode.
2. Local mode works on your machine and your local graph.
3. Remote mode is explicit and uses the HTTP API.
4. Not every command supports remote mode yet.
5. `eshu help` shows the full public command tree.

## Quick local workflow

Index the repo you are in:

```bash
eshu index .
```

List what is indexed:

```bash
eshu list
```

Search for a symbol:

```bash
eshu find name PaymentProcessor
```

Trace callers before you change code:

```bash
eshu analyze callers process_payment
```

## Quick remote workflow

Use a deployed service directly:

```bash
eshu workspace status --service-url https://eshu.qa.example.test --api-key "$ESHU_API_KEY"
```

Or store a profile once:

```bash
eshu config set ESHU_SERVICE_URL_QA https://eshu.qa.example.test
eshu config set ESHU_API_KEY_QA your-token-here
```

Then use the profile:

```bash
eshu workspace status --profile qa
eshu find name handle_payment --profile qa
eshu admin reindex --profile qa
eshu admin facts replay --profile qa --work-item-id fact-work-123
eshu admin facts list --profile qa --status failed
```

## What works remotely today

These commands support remote mode:

- `eshu index-status`
- `eshu workspace status`
- `eshu admin reindex`
- `eshu admin tuning-report`
- `eshu admin facts replay`
- `eshu admin facts dead-letter`
- `eshu admin facts skip`
- `eshu admin facts backfill`
- `eshu admin facts list`
- `eshu admin facts decisions`
- `eshu admin facts replay-events`
- `eshu find name`
- `eshu find pattern`
- `eshu find type`
- `eshu find variable`
- `eshu find content`
- `eshu find decorator`
- `eshu find argument`
- `eshu analyze calls`
- `eshu analyze callers`
- `eshu analyze chain`
- `eshu analyze deps`
- `eshu analyze tree`
- `eshu analyze complexity`
- `eshu analyze dead-code`
- `eshu analyze overrides`
- `eshu analyze variable`

## What stays local

These are still local-only:

- `eshu index`
- `eshu watch`
- `eshu query`
- `eshu workspace plan`
- `eshu workspace sync`
- `eshu workspace index`
- `eshu mcp *`
- `eshu api start`
- `eshu serve start`
- `eshu neo4j setup`

## Commands that are intentionally removed

These names still appear in older docs, scripts, or muscle memory, but they are
not part of the supported Go CLI contract anymore:

- `eshu clean`
- `eshu delete`
- `eshu add-package`
- `eshu unwatch`
- `eshu watching`
- `eshu ecosystem index`
- `eshu ecosystem status`

Use the Go admin/status flows or the supported indexing commands instead.
Use `eshu component` for optional collector and runtime component packages.

## Use this when you are unsure

- Start with [CLI Reference](cli-reference.md) if you need the full command map.
- Start with [Configuration](configuration.md) if you want environment keys and config details.
- Start with [HTTP API](http-api.md) if you are integrating Eshu into another tool.
