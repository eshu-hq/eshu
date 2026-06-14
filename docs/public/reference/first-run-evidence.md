# First-Run Evidence

The first-run evidence report is a compact, human-readable onboarding artifact
and support/debug packet. It captures what `eshu first-run` proved: the runtime
shape it walked, the endpoints it used, the indexing state it reached, the first
bounded query result, and the next commands to run.

It is **not** a canonical graph export. It is a presentation layer over the
result `first-run` already computed; it never re-runs indexing or queries.

## Generating the report

Add the evidence flags to a normal `first-run`:

```bash
# Print a redacted evidence summary to the terminal after the run.
eshu first-run --report

# Also write a redacted Markdown artifact.
eshu first-run --report --report-out first-run-evidence.md

# Write a JSON artifact instead.
eshu first-run --report-out first-run-evidence.json --report-format json
```

You can also regenerate the artifact later from a saved `--json` envelope,
offline, without re-running onboarding:

```bash
eshu first-run --json > first-run.json
eshu first-run report --from first-run.json --format md
eshu first-run report --from first-run.json --format json --out evidence.json
```

`eshu first-run report` reads the envelope from `--from` or from stdin and
renders the same redacted report.

## Redaction

Redaction is mandatory and applied before any value enters the report model, so
both the terminal summary and the on-disk artifact are safe to share:

- API and MCP endpoints have any embedded `user:password@` credentials replaced
  with `redacted`; an endpoint that does not parse as a URL is masked entirely.
- The selected repository target is reduced to its final path element
  (`.../<name>`) so an absolute host path does not leak a username or private
  layout.
- Tokens and bearer secrets are never recorded; only redacted references appear.

Artifacts are written with owner-only (`0600`) permissions because they may
still contain endpoint hostnames.

## Indexing state

The report states indexing as exactly one of `complete`, `partial`, `stale`, or
`failed`. This label is derived from the first-run readiness verdict and the
completeness it proved, **never** invented from process health. An unknown or
empty completeness collapses to `failed` so the packet never overstates
indexing truth.

## Report contents

| Section | Meaning |
| --- | --- |
| Outcome | `succeeded` only when a bounded query returned; otherwise `incomplete`. |
| Runtime shape | `existing_api`, `local_binaries`, `docker_compose`, or `unknown`. |
| Service / MCP endpoint | Redacted endpoints the run used. |
| Indexing state | `complete` / `partial` / `stale` / `failed`, as above. |
| Indexed repositories / selected target | The redacted target when a complete index was proven. |
| Readiness | The readiness and queue/dead-letter verdict string. |
| First query | Whether the bounded query answered, its summary, and truth metadata. |
| Diagnosis | The classified failure (class, summary, preserved root cause), when present. |
| Missing evidence | Proofs the run did not collect. |
| Next commands | Recommended follow-ups, including any classified recovery steps. |
| Docs | This page plus any docs link the diagnosis attached. |

## Related

- [CLI Reference](cli-reference.md)
- [Local Testing](local-testing.md)
- [Operator Digest Contract](operator-digest.md)
