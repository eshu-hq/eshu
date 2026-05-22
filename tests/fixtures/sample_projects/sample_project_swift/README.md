# Swift Parser Fixture

This fixture exercises Swift parser coverage with a small source-only project.
It is test data, not an application walkthrough.

## Fixture Map

| File | Parser surface |
| --- | --- |
| `Main.swift` | entry point, class instantiation, method calls |
| `User.swift` | protocol, struct conformance, extension |
| `Shapes.swift` | protocol conformance across classes and structs |
| `Vehicles.swift` | enums, associated values, inheritance, overrides |
| `Generics.swift` | generic types, generic functions, associated types |

## What Tests Should Prove

- Classes, structs, protocols, protocol conformance, enums, inheritance,
  extensions, generics, initializers, properties, method calls, and imports are
  discovered consistently.
- Parser changes should preserve source-shape coverage without adding build or
  runtime requirements.
