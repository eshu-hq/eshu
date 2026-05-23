# TypeScript Sample Fixture

Parser, indexing, relationship, and content tests use this directory as
TypeScript and TSX source input. It is test data, not a starter template.

| File | Contract |
| --- | --- |
| `src/types-interfaces.ts` | Interfaces, aliases, unions, intersections, conditional types, and utility types. |
| `src/classes-inheritance.ts` | Classes, access modifiers, inheritance, abstract classes, static members, and overloads. |
| `src/functions-generics.ts` | Function signatures, overloads, generics, constraints, and higher-order helpers. |
| `src/async-promises.ts` | Promises, async/await, async iterators, pools, cache, retry, and timeout helpers. |
| `src/decorators-metadata.ts` | Class, method, property, and parameter decorators plus metadata readers. |
| `src/modules-namespaces.ts` | Named/default exports, re-exports, namespaces, augmentation, and dynamic imports. |
| `src/advanced-types.ts` | Mapped types, recursive types, branded types, tuple helpers, and string utilities. |
| `src/error-validation.ts` | Custom errors, result types, type guards, assertions, and validation schemas. |
| `src/utilities-helpers.ts` | Utility functions for string, array, object, function, date, number, color, and timing shapes. |
| `src/index.ts` | Import surface that ties the fixture modules together. |
| `sample_tsx.tsx` | TSX parser coverage. |
| `package.json`, `tsconfig.json` | Package identity and strict compiler settings for parser experiments. |

Tests should prove deterministic parser output, stable entity identifiers,
import/export discovery, repo-relative paths, and parser tolerance for
decorator, async, generic, and advanced type syntax.
