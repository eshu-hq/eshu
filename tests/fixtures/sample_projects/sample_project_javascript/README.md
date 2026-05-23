# JavaScript Sample Fixture

Parser, indexing, relationship, and content tests use this directory as
JavaScript source input. It is test data, not an application template.

| File | Contract |
| --- | --- |
| `functions.js` | Declarations, expressions, arrows, defaults, rest/destructuring, async, generators, higher-order functions, and IIFEs. |
| `classes.js` | Constructors, methods, accessors, inheritance, overrides, private members, class expressions, and mixins. |
| `objects.js` | Object methods, nested methods, prototype assignments, constructor functions, module/factory patterns, and callbacks. |
| `arrays.js` | Array literals, iteration, higher-order array methods, and destructuring. |
| `asyncAwait.js`, `promises.js` | Async/await, promises, callbacks, and error paths. |
| `importer.js`, `exporter.js` | CommonJS and ESM-style import/export shapes. |
| `fixtures/js/accessors.js` | Getter and setter metadata for JavaScript semantic tests. |
| `dom.js`, `events.js`, `fetchAPI.js` | Browser-shaped APIs that should parse without runtime execution. |
| `errorHandling.js`, `variables.js` | Exception and declaration syntax coverage. |

Tests should prove stable names, line numbers, parameters, source paths,
comments, method-kind metadata, and parser tolerance for browser-shaped and
async/control-flow-heavy files.

Assertions live in Go parser and query tests under `go/internal/parser` and
`go/internal/query`; this README only describes the fixture intent.
