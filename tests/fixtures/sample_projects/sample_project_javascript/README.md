# JavaScript Sample Project

Fixture for JavaScript parser, indexing, relationship, and content-shape tests.
It is test data, not an application template.

## Fixture Map

| File | Coverage |
| --- | --- |
| `functions.js` | declarations, expressions, arrows, defaults, rest/destructuring, async, generators, higher-order functions, IIFEs |
| `classes.js` | constructors, instance/static methods, accessors, inheritance, overrides, private members, class expressions, mixins |
| `objects.js` | object methods, nested methods, prototype assignments, constructor functions, module/factory patterns, callbacks |
| `arrays.js` | array literals, iteration, higher-order array methods, destructuring |
| `asyncAwait.js`, `promises.js` | async/await, promises, callbacks, error paths |
| `importer.js`, `exporter.js` | CommonJS/ESM-style import and export shapes |
| `fixtures/js/accessors.js` | getter and setter metadata used by JavaScript semantic tests |
| `dom.js`, `events.js`, `fetchAPI.js` | browser-shaped APIs that should parse without runtime execution |
| `errorHandling.js`, `variables.js` | exception and declaration syntax coverage |

## What Tests Should Prove

- Function, method, class, accessor, import, and export entities materialize
  with stable names, line numbers, parameters, source paths, and comments.
- JavaScript semantic metadata stays attached to parser output and query
  responses when fixtures include method kind or docstring signals.
- Browser-shaped files and async/control-flow-heavy files parse without
  requiring a browser, network, or package install.

Assertions live in Go parser and query tests under `go/internal/parser` and
`go/internal/query`; this README only describes the fixture intent.
