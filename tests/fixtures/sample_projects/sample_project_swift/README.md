# Swift Parser Fixture

This fixture exercises Swift parser coverage with a small source-only project.
It is test data, not an application walkthrough.

| File | Contract |
| --- | --- |
| `Main.swift` | Entry point, class instantiation, and method calls. |
| `User.swift` | Protocol, struct conformance, and extension. |
| `Shapes.swift` | Protocol conformance across classes and structs. |
| `Vehicles.swift` | Enums, associated values, inheritance, and overrides. |
| `Generics.swift` | Generic types, generic functions, and associated types. |

Tests should prove stable discovery for classes, structs, protocols,
conformance, enums, inheritance, extensions, generics, initializers,
properties, method calls, and imports without adding build requirements.
