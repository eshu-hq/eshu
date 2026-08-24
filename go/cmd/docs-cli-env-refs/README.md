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
flags, dynamic flag names, and wildcard environment-variable prefixes.

A logical line may be a **simple list**, and each of its segments is checked
against its own command (#6108):

```text
list    := segment ( SEP segment )*
SEP     := "|" | "&&" | ";"     unquoted, unescaped, outside a comment
segment := one literal command carrying no list operator of its own
```

So `eshu first-run --json | eshu first-run-benchmark --path local_binary`
resolves `--json` against `first-run` and `--path` against
`first-run-benchmark`. Neither command inherits the other's flags, which is what
makes a stale flag on a later segment fail instead of resolving by accident
against an earlier one.

The grammar is a deliberate under-approximation, and everything outside it keeps
the pre-#6108 behaviour of skipping the whole logical line rather than guessing:
`||`, a background `&`, `|&`, `;;`, an unquoted `(`, `)`, or backtick anywhere
on the line, and an empty segment from a leading, trailing, or doubled
separator. An unquoted subshell or command substitution excludes the line
whether or not it also carries a list operator, so
`eshu docs verify $(echo --flag)` is out of scope rather than resolved against
the command `docs verify $(echo`. Operators inside quoted values, escaped
values, or trailing shell comments are not segment boundaries and do not
exclude the line: a backslash escapes the next character everywhere except
inside single quotes, so the pipe in `eshu docs verify "a\"|b" --stale` is
quoted and `--stale` is still checked. A line with no unquoted list operator or
grouping is parsed exactly as before.

An unresolved flag is reported with the command it was attributed to, so a
failure on a piped or chained example says which segment owns the flag.

## Scope honesty

The skip above is a deliberate choice, so it is measured rather than assumed. A
run that finds nothing to complain about and a run whose scanner quietly stopped
reading shell fences are otherwise identical, so every run reports two numbers:

```text
docs-cli-env-refs: 190 Eshu command segment(s) attributed, 1 Eshu command line(s) skipped as unsupported shell forms
```

Both are asserted, because a number in a summary nobody checks is decoration:

- **Skipped lines** count logical lines that *invoke* `eshu` -- the word in
  command position, optionally behind `VAR=value` assignments or a console
  prompt -- and fell outside the grammar. A line that merely passes `eshu` as
  an argument, such as `docker compose logs eshu 2>&1`, is not one of them,
  because an exact pin turns over-reporting into a gate failure on an unrelated
  docs edit. They are pinned exactly, in both directions, by
  `pinnedSkippedEshuLines`. Growth means a new unparseable example slipped into
  a public page: rewrite it into the supported grammar, or re-pin with the
  reason. A shrink also fails, demanding a re-pin, so the population cannot
  creep back up under a stale number. The pinned line today is a backgrounded
  `eshu graph start ... 2>&1 &` in the profiling runbook.
- **Attributed segments** have a floor, `minAttributedEshuSegments`, not a pin.
  That count moves with ordinary documentation edits, so pinning it would fail
  every docs PR; the floor only has to catch the failure it exists for, a
  scanner whose coverage collapsed while still exiting zero.

`scripts/verify-docs-cli-env-refs.sh` reads `ESHU_DOCS_CLI_ENV_PINNED_SKIPPED_LINES`
and `ESHU_DOCS_CLI_ENV_MIN_ATTRIBUTED_SEGMENTS` so the companion suite can drive
both assertions over scratch fixtures. Leaving them unset — what the real gate
does — uses the code-owned values.

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
