# CLI K.I.S.S.

Use this page when you need the short mental model for `eshu`.

If you only remember five things about `eshu`, make it these:

1. `eshu` has local runtime commands and API-backed read/admin commands.
2. Local runtime commands start or manage processes on your machine.
3. API-backed commands call the HTTP API through flags, persisted config,
   environment variables, or `http://localhost:8080`.
4. Some API-backed commands do not register per-command remote flags yet.
5. `eshu help` shows the current public command tree.

## Quick Local Workflow

Index the repo you are in and wait for readiness:

```bash
eshu scan .
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

## Quick Remote Workflow

Use a command that honors remote flags directly:

```bash
eshu find name handle_payment --service-url https://eshu.qa.example.test --api-key "$ESHU_API_KEY"
```

Or store the default API target once:

```bash
eshu config set ESHU_SERVICE_URL https://eshu.qa.example.test
eshu config set ESHU_API_KEY your-token-here
```

Then API-backed commands can use the persisted target:

```bash
eshu workspace status
eshu index-status
eshu find name handle_payment
eshu admin facts list --status failed
eshu admin facts replay --work-item-id fact-work-123
```

`eshu workspace status` and `eshu index-status` register remote flags in help,
but their current handlers ignore those flags. Use persisted config or process
environment for those two commands.

## Commands With Remote Flags

These commands call the API and honor `--service-url`, `--api-key`, and
`--profile`:

- `eshu scan`
- `eshu admin reindex`
- `eshu admin tuning-report`
- `eshu admin facts replay`
- `eshu admin facts dead-letter`
- `eshu admin facts skip`
- `eshu admin facts backfill`
- `eshu admin facts list`
- `eshu admin facts decisions`
- `eshu admin facts replay-events`
- `eshu docs verify` for API-backed container-image truth checks
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
- `eshu map`
- `eshu trace service`

## API-Backed Without Remote Flags

These commands call the API through persisted config, process environment, or
the local default URL:

- `eshu list`
- `eshu stats`
- `eshu query`
- `eshu workspace plan`
- `eshu workspace sync`
- `eshu workspace index`

## Local Runtime Commands

These commands manage local binaries, local processes, or local configuration:

- `eshu index`
- `eshu watch`
- `eshu workspace watch`
- `eshu graph`
- `eshu install nornicdb`
- `eshu mcp`
- `eshu api start`
- `eshu serve start`
- `eshu neo4j setup`
- `eshu config`
- `eshu component`

## Compatibility Stubs

These names remain visible for older scripts, but they return replacement
guidance instead of doing old Python-era work:

- `eshu clean`
- `eshu delete`
- `eshu add-package`
- `eshu unwatch`
- `eshu watching`
- `eshu ecosystem index`
- `eshu ecosystem status`

Use the Go admin/status flows, supported indexing commands, or `eshu component`
for optional collector and runtime component packages.

## When You Are Unsure

- Use [CLI Reference](cli-reference.md) for the full command map.
- Use [Configuration](configuration.md) for environment keys and config details.
- Use [HTTP API](http-api.md) when integrating Eshu into another tool.
