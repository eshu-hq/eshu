# C# Parser Fixture

This fixture exercises the C# parser against a small project with one
`Example.App` assembly. It is test data, not an example application.

## Fixture Map

| Path | Parser surface |
| --- | --- |
| `src/Example.App/Program.cs` | entry point, object creation, service calls |
| `src/Example.App/OuterClass.cs` | nested classes |
| `src/Example.App/Models/*.cs` | classes, enums, records, structs, operators |
| `src/Example.App/Services/*.cs` | interfaces, implementations, attributes |
| `src/Example.App/Utils/*.cs` | static helpers, generics, LINQ, file I/O calls |
| `src/Example.App/Attributes/*.cs` | custom attribute definitions |

## What Tests Should Prove

- Namespace and type discovery stay stable across classes, interfaces,
  records, structs, enums, nested classes, and static classes.
- Method signatures, constructors, properties, and private helpers are
  discoverable without inventing calls.
- `using` directives, internal project references, object creation, LINQ calls,
  and attribute usage produce the expected graph facts.
- Parser changes preserve fixture intent without depending on a real build or
  runtime execution.
