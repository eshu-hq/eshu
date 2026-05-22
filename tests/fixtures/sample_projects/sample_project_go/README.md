# Go Sample Project

This fixture exercises Go parser, indexing, relationship, and content shape
behavior. It is test data, not an application template.

## Fixture Map

| File | Coverage |
| --- | --- |
| `basic_functions.go` | functions, returns, variadics, closures, recursion, `defer`, `panic`, `recover`, `init` |
| `structs_methods.go` | structs, pointer/value receivers, constructors, embedding, method chaining |
| `interfaces.go` | interfaces, embedding, implementations, assertions, type switches |
| `goroutines_channels.go` | goroutines, channels, `select`, worker pools, mutexes, wait groups |
| `error_handling.go` | custom errors, wrapping, sentinels, `errors.Is`, `errors.As`, validation |
| `generics.go` | generic functions, generic types, constraints, stacks, queues, caches |
| `embedded_composition.go` | embedding, method promotion, interface embedding, composition |
| `advanced_types.go` | custom types, aliases, enum patterns, maps, slices, tags, function/channel types |
| `packages_imports.go` | stdlib imports, aliases, blank imports, package initialization |
| `util/helpers.go` | subpackage helpers and package-level utility functions |
| `go.mod` | Go module identity for fixture indexing |

## What Tests Should Prove

- Package and import relationships are stable across root files and the `util`
  subpackage.
- Functions, methods, structs, interfaces, and generic declarations materialize
  with stable identifiers.
- Concurrency and error-handling syntax does not break parsing or content
  shaping.
- Fixture graph/content tests can cite repo-relative source paths.

## Local Fixture Commands

```bash
go test ./...
go build ./...
```

These commands are optional for Eshu repository tests unless a test explicitly
compiles this fixture.
