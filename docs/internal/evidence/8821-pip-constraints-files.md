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
consumption arm already drop. The readiness unsupported-source arm does not
read it (#9073). There is no new fact kind. The change adds one row key,
`source_cache_withheld`, and one reader change in `content/shape`
(`entitySourceCache`), described under Credential scope.

## Credential scope

The scrub covers the parsed row and the entity `source_cache`. The shaper
slices `source_cache` from the file's line range, so a field-level scrub alone
left the raw line in the `content_entity` fact (review finding F1). Every
constraints row now sets `source_cache_withheld`, which `entitySourceCache`
honors, and stores no `raw`. `TestPipConstraintsCredentialsStayOutOfEntityFacts`
runs the production snapshotter and fact builder and requires that no
non-`content` fact holds the secret. The `content` fact keeps the whole file,
credentials included, until the content-store policy (#8812) covers this
parser; the same test pins that.

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
