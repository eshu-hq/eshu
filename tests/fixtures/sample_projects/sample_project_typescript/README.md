# TypeScript Sample Project

This fixture exercises TypeScript parser, indexing, relationship, and content
shape behavior. It is test data, not a starter template.

## Fixture Map

| File | Coverage |
| --- | --- |
| `src/types-interfaces.ts` | interfaces, aliases, unions, intersections, conditional types, utility types |
| `src/classes-inheritance.ts` | classes, access modifiers, inheritance, abstract classes, static members, overloads |
| `src/functions-generics.ts` | function signatures, overloads, generics, constraints, higher-order helpers |
| `src/async-promises.ts` | promises, async/await, async iterators, pools, cache, retry and timeout helpers |
| `src/decorators-metadata.ts` | class, method, property, and parameter decorators plus metadata readers |
| `src/modules-namespaces.ts` | named/default exports, re-exports, namespaces, augmentation, dynamic imports |
| `src/advanced-types.ts` | mapped types, recursive types, branded types, tuple/string utilities |
| `src/error-validation.ts` | custom errors, result types, type guards, assertions, validation schemas |
| `src/utilities-helpers.ts` | string, array, object, function, date, number, color, and timing helpers |
| `src/index.ts` | imports and demonstrates the fixture modules |
| `sample_tsx.tsx` | TSX parser coverage |
| `package.json`, `tsconfig.json` | Node package and strict TypeScript compiler settings |

## What Tests Should Prove

- Parser output stays deterministic for TypeScript and TSX sources.
- Import/export relationships are discovered across module styles.
- Class, function, method, interface, and type alias entities materialize with
  stable identifiers.
- Decorator, async, generic, and advanced type syntax does not break indexing.
- Content and code-search fixtures can cite repo-relative file paths from this
  project.

## Local Fixture Commands

The fixture includes package metadata for local parser experiments:

```bash
npm install
npm run build
npm test
```

Those commands are optional for Eshu repository tests unless a specific test
case says it compiles or executes this fixture.
