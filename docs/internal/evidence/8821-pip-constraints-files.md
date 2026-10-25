# #8821: pip constraints files as version-policy rows

Part of #8807. This note holds the evidence for admitting `constraints.txt`,
`constraints-*.txt` and `constraints_*.txt` to the pip requirements parser.
Figures come from a throwaway benchmark (not committed) on linux/arm64, 16
threads, Go 1.26.10, `-count=5`, with other jobs on the host.

## Decision

A pip constraint limits the version a package gets if something else installs
it; it does not install it. The rows therefore reuse the NuGet central-pin
contract (#8824): `config_kind=dependency` with `central_version_declaration`
true, which the consumption extractor, the Postgres sidecar and the readiness
SQL already drop. No fact kind, payload key or reader changes.

## Performance Evidence

Performance Evidence: `Registry.LookupByPath` gained one base-name check per
walked file, `isPipConstraintsName`: one `.txt` suffix test, then one equality
and two prefix tests only for a `.txt` name.

| Benchmark | Time | Allocs |
|---|---|---|
| `isPipConstraintsName("main.go")` | 1.95 ns/op | 0 |
| `LookupByPath("src/app/main.go")` with the check in place | 195 to 200 ns/op | 1 (16 B) |

The 16-byte allocation is the existing defensive copy of the definition, not
the new check. The documentation path gained one string comparison. No query,
queue, graph-write, lease or worker path changes. Input shape: one path string
per call; the parse of a constraints file costs the same as a requirements
file of the same size.

## Observability Evidence

No-Observability-Change: the change adds no metric, span, log key or status
field. The existing parse and content-entity counters count the new rows under
the same labels. `gitGenerationContentVersion` moved to `git-collector.v11`, so
each repository stores one new generation on its next sync; the pinned fixture
gained `manifests/python/constraints-ci.txt` and the pin was rewritten with
`-update-content-version-pin`.
