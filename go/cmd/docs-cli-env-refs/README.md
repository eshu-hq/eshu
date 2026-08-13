# docs-cli-env-refs

## Purpose

This command checks concrete `ESHU_*` names and command-specific long flags in
public documentation against Eshu's environment registry and the help tree from
a built Eshu CLI. It is the offline implementation behind the blocking
documentation reference gate added for #6023.

## Ownership boundary

This package owns public Markdown scanning, CLI help collection, unresolved
reference comparison, and baseline validation. It does not validate prose,
execute documented commands, or change any Eshu service, datastore, graph,
queue, or worker behavior.

## Exported surface

The package is a command and has no exported Go API. Its godoc contract is in
[doc.go](doc.go); callers use the `docs-cli-env-refs` binary through
`scripts/verify-docs-cli-env-refs.sh`.

## Dependencies

Environment-variable truth comes from `internal/envregistry`. Flag truth comes
from command-specific Cobra help produced by a real built `eshu` binary rather
than a copied allowlist. Files under the configured documentation root are read
through `os.OpenRoot`; a link that escapes that root fails the scan.

## Telemetry

This package emits no runtime metrics or spans. It writes process-local status
and failure diagnostics to standard error.

## Gotchas / invariants

The scanner is precision-first. It includes indented or list-contained
`bash`, `sh`, `shell`, and `console` fences, including literal single- or
double-quoted long flags. It skips prose, inline flags, non-shell fences, short
flags, dynamic flag names, wildcard environment-variable prefixes, and whole
logical lines containing an unquoted shell-list operator such as a pipeline or
`&&` list. Operators inside quoted values, escaped values, or trailing shell
comments do not exclude the line.

When a command starts with a root flag before its subcommand, the scanner checks
the leading root flag but deliberately skips later command-local flags on that
logical line. Documentation should use `eshu <command> --flag` when it needs
complete command-local coverage.

Known legacy misses live in `scripts/docs-cli-env-refs-baseline.txt`. The frozen
`scripts/docs-cli-env-refs-ceiling.txt` records the initial #6023 debt set, so
adding the same unresolved reference to both a page and the mutable baseline
still fails. An exact code-owned count and canonical membership digest pin the
immutable ceiling at the original 772 references. Adding, removing, or replacing
ceiling membership fails even if the documentation and mutable baseline change
in the same patch. Only the mutable baseline burns down: update mode can remove
resolved entries but cannot add debt, and
malformed baseline or ceiling files fail closed. A root-command flag is stored
as `<root>::--flag` so baseline updates can be read back without losing its
command scope.

## Related docs

- [Local testing](../../../docs/public/reference/local-testing.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [Environment variables](../../../docs/public/reference/environment-variables.md)
- [Scripts package notes](../../../scripts/README.md)
