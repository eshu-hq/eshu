# Go Sample Fixture

Parser and graph tests use this directory as Go source input. It is test data,
not an application template.

| File | Contract |
| --- | --- |
| `basic_functions.go` | functions, returns, closures, recursion, `defer`, `panic`, `recover`, and `init`. |
| `structs_methods.go` | structs, constructors, receiver methods, embedding, and method chaining. |
| `interfaces.go` | interface declarations, implementations, assertions, and type switches. |
| `goroutines_channels.go` | goroutines, channels, `select`, worker pools, mutexes, and wait groups. |
| `error_handling.go` | custom errors, wrapping, sentinels, `errors.Is`, `errors.As`, and validation. |
| `generics.go` | generic functions, generic types, constraints, stacks, queues, and caches. |
| `embedded_composition.go` | embedding, method promotion, interface embedding, and composition. |
| `advanced_types.go` | custom types, aliases, enum patterns, maps, slices, tags, and function/channel types. |
| `packages_imports.go` | stdlib imports, aliases, blank imports, and package initialization. |
| `util/helpers.go` | subpackage helpers for package and import relationship tests. |
| `go.mod` | Module identity for fixture indexing. |

Tests should prove stable package/import relationships, entity identifiers,
repo-relative source paths, and parser tolerance for concurrency, generics, and
error-handling syntax.
