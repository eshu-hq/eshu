# C# Parser Fixture

This fixture exercises the C# parser against a small project with one
`Example.App` assembly. It is test data, not an example application.

| Path | Contract |
| --- | --- |
| `src/Example.App/Program.cs` | Entry point, object creation, and service calls. |
| `src/Example.App/OuterClass.cs` | Nested classes. |
| `src/Example.App/Models/*.cs` | Classes, enums, records, structs, and operators. |
| `src/Example.App/Services/*.cs` | Interfaces, implementations, and attributes. |
| `src/Example.App/Utils/*.cs` | Static helpers, generics, LINQ, and file I/O calls. |
| `src/Example.App/Attributes/*.cs` | Custom attribute definitions. |

Tests should prove stable namespace/type discovery, method signatures,
constructors, properties, private helpers, `using` directives, project
references, LINQ calls, and attribute usage without requiring a real build.
