# #8822: `gradle-wrapper.properties` under the metadata policy

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md).
This note holds the decision for the wrapper file, which follows `.npmrc`
([8978](8978-npmrc-registry-url.md)).

## Decision

`gradle-wrapper.properties` is the second `metadata` name. A
`distributionUrl` can carry `user:password@` userinfo, and a `gradle` definition
binding would store it. It is its own exact-name `gradle_wrapper` definition
bound to `metadata`: an empty-body row with the path and the source digest.
Parser output is outside the policy, so the wrapper parser keeps one thing from
the file, the version in the archive name (`gradle-<version>-bin.zip` or
`-all.zip`), plus the `bin` or `all` type. It emits no URL, host, userinfo,
query, or other property, and it ignores `IndexSource`, so even a caller that
asks for the source gets none. A value outside that grammar yields no row and is
counted. The proof is
`TestGradleWrapperSeededCredentialReachesNoFactUnderTheProductionBinding` in
`go/internal/collector/repo/git`, a full-stream scan of every emitted fact for
seeded userinfo, query, fragment, and a second-property credential, with
`TestGradleWrapperScanFindsTheSeedsWhenTheBodyIsStored` as the liveness control.

## Performance Evidence

Performance Evidence: the default registry was already governed by the `.npmrc`
binding, so the per-file policy lookup is not new. The exact name adds one entry
to the registry index and no work for other files. A wrapper file costs one
linear pass over at most 64 KiB. `BenchmarkParseConfig/gradle_wrapper` parses a
10,000-line file (about 35 KB, under the bound) in 0.66 ms, 125,928 B/op and
41 allocs/op, on a shared arm64 host.

## Observability

No-Observability-Change: the parser emits no metric, span, or log. The
`gradle_wrapper_state` payload envelope carries `state` and `unsupported_count`
for a file whose `distributionUrl` the grammar could not read.
