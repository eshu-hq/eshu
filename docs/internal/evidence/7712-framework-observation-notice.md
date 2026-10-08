# Production Framework Observation For The Dead-Code Notice (#7712)

This note records the proof for #7712, which gives the dead-code
no-root-model notice (#7595) production framework observation: per-result
`framework` metadata now flows from parser facts through entity metadata,
the graph, and result metadata into
`analysis.frameworks_without_root_model`, and the modeled-framework table
covers go, groovy, and python with a root-model census. This change also
closes #7595: go gin/echo/fiber and python django/drf/aiohttp/tornado are
real observed frameworks with no root model, so the notice fires for them.

## What changed

Parser producers plus the query-side census table. No reducer, projector,
storage, schema, API, MCP, or telemetry code changed: `framework` rides the
existing generic metadata pass-through (projector clones entity metadata,
the graph writer allowlists the key, semantic rows and the result taxonomy
already map it), following the groovy precedent.

- **PHP evidence-precise tags** (`go/internal/parser/php/dead_code_roots.go`,
  `parser.go`). `phpDeadCodeFramework` tags a function only where framework
  evidence touches it: a Symfony Route attribute on the method, a WordPress
  hook callback name, or a ZF1 controller action. The conditions mirror the
  root-kind classifier exactly. Zero or several claimants leave the function
  untagged: ambiguous stays silent rather than guessing.
- **Go propagation** (`go/internal/parser/golang/language.go`,
  `framework_routes.go`). `goSingleFileFramework` propagates the file's
  route frameworks to every function fact when exactly one is observed;
  multi-framework files stay untagged.
- **Python propagation** (`go/internal/parser/python/language.go`,
  `framework_gathered.go`). Same exactly-one/skip-on-multiple rule as go,
  applied as a post-pass because python builds semantics after the walk.
- **Census table** (`go/internal/query/codemodel/code_dead_code_language_maturity.go`).
  go {net_http, chi} — chi has no chi-specific kind but chi handlers match
  `go.net_http_handler_signature`, so listing it unmodeled would fire a
  false notice; groovy {jenkins}; python {fastapi, flask} (decorator
  roots). Observed-but-unmodeled: go gin/echo/fiber, python
  django/drf/aiohttp/tornado. Modeled-but-unobserved (cobra,
  controller-runtime, celery, click, typer) carry a same-change
  discipline note: a future producer must add its table entry together.

## Edge cases

| Case | Handling | Proof |
|---|---|---|
| ZF1 action with a Symfony Route attribute | Two claimants → untagged | `TestDefaultEngineParsePathPHPSkipsFrameworkTagOnMultipleEvidence` |
| Plain PHP function / helper method | No evidence → untagged | symfony/WP/ZF1 tag tests assert siblings untagged |
| Go file with gin + echo routes | Multi → untagged | `TestDefaultEngineParsePathGoSkipsFrameworkTagOnMultipleFrameworks` |
| Go file without routes | No semantics → untagged | `...GoLeavesFrameworkUnsetWithoutRoutes` |
| Python django + drf file | Multi → untagged | `...PythonSkipsFrameworkTagOnMultipleFrameworks` |
| chi handlers | Modeled via signature coverage, documented | table test asserts chi silent |
| python fastapi/flask | Modeled (decorator roots) | table test asserts silent; django fires |
| Groovy non-jenkins framework | Fires (only jenkins modeled) | table test asserts gradle fires |

## Verification

- RED then GREEN for five suites: php tags, go propagation, python
  propagation, the census table, and the end-to-end notice test.
- `TestHandleDeadCodeReportsProducerObservedFrameworks` parses a real gin
  file and a real symfony file, builds graph rows from the parsed function
  facts' framework values, and asserts the handler reports
  `frameworks_without_root_model.go == [gin]` with a `go(gin)` note while
  php stays silent.
- Full packages green: parser/php, parser/golang/..., parser/python,
  query/codemodel, query/codequery/deadcode.
- Contract: patch-level. `framework` is an existing optional function-fact
  key (groovy precedent); no factschema, fixture-pack, or registry file
  changed, and no typed decode reads it.
- B-7 neutral: the gate asserts dead-code envelope/maturity values only,
  never per-result metadata or notes; the corpus carries net_http go routes
  (modeled) and no php route markers. Property additions change no
  node/edge counts.
- Performance: parse-time only — one map lookup per function (php) or one
  map insert per function (go/python), same order as the existing
  root-kind loop; no hot-path, backend, or query-shape change, so no
  benchmark delta is claimed.
