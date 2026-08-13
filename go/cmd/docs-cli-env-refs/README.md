# docs-cli-env-refs

Blocking public-documentation reference checker for #6023. It compares concrete
`ESHU_*` citations with `internal/envregistry` and long flags from conservative
fenced `eshu` shell commands with the actual built CLI's command-specific Cobra
help tree.

The scanner is deliberately precision-first. It includes indented,
list-contained shell fences and literal single- or double-quoted long flags. It
skips prose and inline flags, non-shell fences, and logical lines containing an
unquoted shell-list operator (`|`, `&`, or `;`), including pipelines and `&&`
lists. It also skips short flags, dynamic flag names, and wildcard
environment-variable prefixes. Known legacy misses live in
`scripts/docs-cli-env-refs-baseline.txt`; new misses fail.

When a command starts with a root flag before its subcommand, v1 validates the
leading root flag but deliberately skips later command-local flags on that
logical line. Reorder the example to `eshu <command> --flag` for complete v1
coverage.

No-Regression Evidence: this is an offline CI verifier. It starts no Eshu
service, opens no datastore, and does not change request, graph, queue, worker,
or runtime behavior.

No-Observability-Change: verifier diagnostics are process-local stderr only;
no runtime metrics, spans, or structured service logs change.
