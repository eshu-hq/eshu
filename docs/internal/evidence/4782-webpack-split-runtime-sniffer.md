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

## Performance Evidence:

Measured by #4782 against a real bundle, not a fixture:

| Case | Parse time |
| --- | --- |
| WordPress/Gutenberg `wp-includes/js/dist/components.js` (~2.7MB), before | **~15.9s** |
| A normal source file, same run | ~0.071s (the ~224x baseline the issue cites) |
| Same bundle, after | not parsed — skipped at discovery |

The miss was systematic rather than one bad file: every WordPress/Gutenberg
build sampled in that investigation was webpack, and every one was missed.

The saving is the whole parse. A skipped file costs the 8KiB prefix read the
sniffer already performs, so the after-cost is the sniff itself.

## No-Regression Evidence:

The two required tokens are unchanged, so nothing that matched before stops
matching. The webpack 4 (`installedModules`) and webpack 5 full-runtime
fixtures, plus the Rollup, esbuild and Parcel sniffs, are pre-existing tests and
all still pass: `go test ./internal/collector ./internal/collector/discovery`
→ exit 0.

The widening direction is guarded by a negative fixture that puts `webpack`,
`webpackBootstrap` and `__webpack_modules__` in comments in a 12000-line
hand-written file. It is not skipped, because it has no `/******/` banner.

## No-Observability-Change:

No metric, span or log changes. Files skipped by the sniffer are already counted
under the existing `FilesSkippedByContent["generated-webpack"]` discovery stat;
this change moves bundles into that existing counter rather than adding a
signal. An operator watching that stat sees the count rise for repositories that
ship split-runtime webpack output, which is the intended effect.

Refs #4782
