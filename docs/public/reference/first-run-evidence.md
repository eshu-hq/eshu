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

Every value is scrubbed before it enters the report model, so the terminal
summary and the on-disk artifact carry the same redacted text.

The endpoint and target fields are rewritten like this:

- An API or MCP endpoint loses any embedded `user:password@` credential, and
  loses the value of any query parameter whose name looks like a credential
  (`token`, `api_key`, `access_token`, `authorization`, and similar), replaced
  with `redacted`. "Any" means any: pairs are separated by `?`, `&` or `;`, so a
  second query string nested inside a parameter value
  (`?next=/v0/y?api_key=…`) is walked too. Each of those separators, and the `=`
  that joins a name to its value, counts whether it is written literally or
  percent-encoded, so `?a=1&b=%3Ftoken%3D…` — how an HTTP client writes a nested
  URL — is walked exactly like `?a=1&b=?token=…`. Hex case does not matter, and a
  percent-encoded name (`api%5Fkey`) is decoded before the match. The escapes are
  copied through in the spelling they arrived in, so you can still match the
  endpoint against your own config. A parameter with no value at all
  (`?token` or `?token=`) is left alone rather than shown as `token=redacted`,
  which would report a credential the URL never carried.
- The **fragment** — everything after `#` — is dropped whole. A fragment never
  reaches the server, so it tells you nothing about which target the report
  describes, and it is where an OAuth implicit-grant callback puts its token
  (`https://app.example.com/cb#access_token=…`).
- The scheme, host, path, and every other query parameter stay, so you can still
  tell which target the report describes. An endpoint that does not parse as a
  URL is masked entirely.
- The selected repository target is reduced to its final path element
  (`.../<name>`) so an absolute host path does not leak your username or your
  private directory layout.

The free-text fields go through the same pass, and it rewrites them.
`readiness`, `query_summary`, `truth` (at every depth, including values nested
inside objects and arrays), the diagnosis summary, its preserved cause, its
recovery steps, `next_commands`, and `docs_links` are all scrubbed. An endpoint
or repository path that was composed into one of those sentences comes out in
the same redacted form as the field it was built from.

A credential does not need a URL around it to be found. The text between any
absolute URLs is scanned for a credential-shaped pair written as `key=value` or
as a header (`key: value`), and the pair is removed — name and all — and
replaced with `[redacted]`. This is what catches a rejected request body quoted
back into the diagnosis, such as
`api returned 401; body was {"error":"unauthorized","api_key":"…"}`, which
carries no `scheme://` anywhere for a URL rule to find. It is the same scan
`eshu report capture` runs over a support bundle's reporter note, so the two
artifacts remove the same things.

The header form has no safe inner boundary, because an HTTP header value may
contain spaces. It removes from the key to the **end of the line**, so a second
`-H` on the same line goes with it. Over-removal is the side this errs on.

That has a cost, and it lands on the commands.

A suggested next command is rewritten along with everything else, so
`eshu story /home/alice/work/repo` is recorded as `eshu story .../repo`. Read
`next_commands` as a description of what to run against your own paths, not as
lines to paste.

The removal does not spare a placeholder. Nothing checks whether a value looks
like a secret, so a fixed instruction written as
`Set a matching token: export ESHU_API_KEY=<server token>` is recorded as
`Set a matching [redacted]` — the pair goes name and all, and the `token:` in
front takes the rest of the line with it. Eshu's own recovery steps are phrased
to avoid that shape, so they reach the artifact intact and still name the
variable to set. If you are writing a step of your own that a support artifact
will carry, phrase it without a `key=value` pair rather than expecting the scan
to spare it.

### What redaction does not catch

The rules recognize structure: a URL, an absolute path, a credential-shaped
parameter name. Nothing here judges whether a piece of text looks like a
secret, so these reach the artifact as written:

- A credential in a URL **path segment**, such as
  `https://host/x/sk-live-abc/story`. This covers a `#` written as `%23`
  (`https://host/cb%23access_token=…`): a percent-encoded `#` is a literal
  character in the path, not a fragment, so the fragment rule above does not
  reach it.
- A credential under a parameter name the rule does not match, such as
  `?session=sk-live-abc`.
- A bare secret in prose with no key beside it, such as
  `authenticated with sk-live-abc`.
- A credential written **after a URL that follows its own key**, such as
  `Authorization: Bearer https://host/x sk-live-abc`. The URL and the prose
  around it are scrubbed separately, and a URL ends the stretch of prose the
  header rule can reach, so the header goes and the trailing word stays. A
  credential before the URL, or on a line with no URL, is removed.
- A separator encoded **twice**, such as `?next=%253Ftoken%253D…`. The unwrap
  runs exactly one layer, on purpose: `%253F` asks for the literal text `%3F`,
  so a second layer would describe a request no server received, and a loop
  would let the input decide how long it runs.

Artifacts are written with owner-only (`0600`) permissions because they still
contain endpoint hostnames.

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

This report is a presentation layer over one `first-run` result, not a
portable artifact. For a share-safe snapshot of the whole stack's state —
repository count, queue health, semantic provider posture — export a
[Portable Evidence Bundle](evidence-bundle.md) with `eshu evidence bundle
export --live` instead.

- [CLI Reference](cli-reference.md)
- [Local Testing](local-testing.md)
- [Operator Digest Contract](operator-digest.md)
- [Portable Evidence Bundle](evidence-bundle.md)
