# #4782 — webpack chunks with a split runtime were parsed, not skipped

## What the sniffer missed

Discovery reads the first 8KiB of a `.js` file to decide whether it is generated
output worth skipping. For webpack 5 it required four tokens: `webpackBootstrap`,
the `/******/` banner, `__webpack_modules__`, **and** both
`__webpack_module_cache__` and a literal `function __webpack_require__`.

Webpack emits the last two conditionally. From `JavascriptModulesPlugin`:

```js
if (useRequire || moduleCache) { /* __webpack_module_cache__ */ }
if (useRequire)                { /* the __webpack_require__ function */ }
```

A build that extracts its runtime into a separate chunk — `optimization.runtimeChunk`,
an ordinary setting — needs neither in any other chunk. Every such chunk failed
the sniff and went to a full tree-sitter parse.

## The 15.9s figure in the issue belongs to a different fix

#4782 is motivated by a ~2.7MB WordPress/Gutenberg bundle parsing in ~15.9s,
about 224x a normal file. That hazard was already closed by **#4766**.

`go/internal/parser/javascript/javascript_language.go` carries
`jsParseByteCap = 1 << 20`. Any JavaScript-family file over 1 MiB is never
handed to tree-sitter — it returns a payload carrying a `js_parse_bounded` row
and nothing else. `go/internal/parser/parse_bytecap_test.go` cites that same
15.9s / 2.7MB / 224x measurement as its own justification.

Measured here rather than assumed: a 2.06MB minified-shaped bundle parses in
**2.4–4.1ms** under production options, and the only bucket it produces is
`js_parse_bounded: 1`. There is no seconds-scale parse cost left above 1 MiB
for this change to save, so that number is not evidence for this change.

## Performance Evidence:

The sniffer still earns its place in the window between its own floor and the
parse cap:

    256 KiB (generatedJavaScriptBundleMinBytes) .. 1 MiB (jsParseByteCap)

A generated bundle in that window sits under the cap, so it is fully parsed.
Measured on a 0.58MB webpack split-runtime bundle, Apple M-series, this
checkout:

| Case | Elapsed |
| --- | --- |
| Parse, bare options | 1.885s |
| Parse, production options (`IndexSource: true`) | 1.899s |
| Parse, production options + `EmitDataflow` | 2.531s |
| Sniffer decision instead (file skipped at discovery) | **0.199ms** |

The same parse also emits **6,800 `functions` and 10,200 `function_calls`**
from generated output. Those are phantom graph entities, which is a correctness
argument for the skip independent of the timing one.

Method: a throwaway test in `internal/collector` wrote a minified-shaped bundle
(one very long line of nested function expressions) to a temp directory, timed
`parser.DefaultEngine().ParsePath` under each option set, then timed
`generatedNativeSnapshotSkipReason` on the same file. Deleted after measuring.

A first attempt used 93,000 identical short lines and measured 11.7ms for
2.66MB. That fixture is not a bundle — "2.7MB of JavaScript" is not one thing,
and the nested single-line shape is what costs tree-sitter. The numbers above
use the second shape.

## Both sync paths, not just full discovery

`filterGeneratedNativeSnapshotFiles` used to have exactly one caller,
`git_snapshot_discovery.go:330`, on the full discovery path. The delta-sync
resolver `resolveNativeSnapshotFileSetForTargets` — the explicit
`--file-targets` path — never applied it.

So one webpack bundle was skipped when a full discovery found it and indexed
when a delta sync touched it. The same file, two answers, decided by which sync
mode happened to run. That gap predates this change; it is closed here.

The delta path now applies the same filter and reports skips through the
`DiscoveryStats` the caller already holds, so a delta-sync skip lands in the
same `FilesSkippedByContent["generated-webpack"]` counter rather than going
silent. Without the filter the regression test fails with:

```
a delta sync kept a generated webpack bundle; the full discovery path skips
the same file, so the graph depends on which sync mode ran
```

## No-Regression Evidence:

The two required tokens are unchanged, so nothing that matched before stops
matching. The webpack 4 (`installedModules`) and webpack 5 full-runtime
fixtures, plus the Rollup, esbuild and Parcel sniffs, are pre-existing tests and
all still pass:

```
go build ./...                                                       exit 0
go test ./internal/collector ./internal/collector/discovery -count=1  ok
```

The build is included because the delta-path fix changes a shared signature.

The widening direction is guarded by a negative fixture that puts `webpack`,
`webpackBootstrap` and `__webpack_modules__` in comments in a hand-written file.
It is not skipped, because it has no `/******/` banner.

Both fixtures are pinned above the 256 KiB floor by an explicit check. Without
it the negative case would pass vacuously — a file under the floor is kept
whatever the sniffer decides. Seeding a shrunken fixture fails with:

```
hand-written webpack mention fixture is 2694 bytes, below the 262144-byte
floor; the sniffer is never consulted and the assertions below prove nothing
```

## No-Observability-Change:

No metric, span or log changes. Files skipped by the sniffer are already counted
under the existing `FilesSkippedByContent["generated-webpack"]` discovery stat;
this change moves bundles into that existing counter rather than adding a
signal. An operator watching that stat sees the count rise for repositories that
ship split-runtime webpack output, which is the intended effect.

Refs #4782
