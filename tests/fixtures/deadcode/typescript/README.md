# TypeScript Dead-Code Fixture

Expected symbols:

- `unused`: `unusedTypedHelper`
- `direct_reference`: `formatAccount`
- `entrypoint`: `bootstrap`
- `public_api`: `AccountService`, `AccountShape`, `PublicResult`
- `framework_root`: `GET`, `saveAccount`
- `semantic_dispatch`: `loadPlugin`
- `excluded`: `generatedTypedHelper`, `testOnlyTypedHelper`
- `ambiguous`: `ambiguousDecoratorTarget`

Notes:

- Next.js route exports and Express route registrations reuse the JavaScript
  family root model.
- Type exports, decorators, dynamic imports, and reflection-like property access
  remain derived or ambiguous evidence, not exact cleanup proof.
