# TypeScript Sample Fixture

TypeScript and TSX source corpus for parser and indexing tests. This directory
is test data, not a starter app.

## File Map

| Path | Contract |
| --- | --- |
| `package.json` | Package identity for fixture consumers. |
| `tsconfig.json` | Strict compiler settings used by parser experiments. |
| `src/index.ts` | Import surface tying the fixture modules together. |
| `src/types-interfaces.ts` | Interfaces, aliases, unions, intersections, conditional types. |
| `src/classes-inheritance.ts` | Classes, inheritance, access modifiers, abstract/static members. |
| `src/functions-generics.ts` | Function signatures, overloads, generics, constraints. |
| `src/async-promises.ts` | Promises, async/await, async iterators, retry/timeout helpers. |
| `src/decorators-metadata.ts` | Class, method, property, and parameter decorators. |
| `src/modules-namespaces.ts` | Exports, re-exports, namespaces, augmentation, dynamic imports. |
| `src/advanced-types.ts` | Mapped, recursive, branded, tuple, and template-string types. |
| `src/error-validation.ts` | Custom errors, result types, guards, assertions, schemas. |
| `src/utilities-helpers.ts` | Utility functions across common value shapes. |
| `sample_tsx.tsx` | TSX parser input. |

Expected truth: fixture consumers should preserve deterministic parser output,
stable entity identifiers, import/export discovery, and repo-relative paths.
