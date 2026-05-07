# JavaScript Dead-Code Fixture

Expected symbols:

- `unused`: `unusedLocalHelper`
- `direct_reference`: `formatUser`
- `entrypoint`: `main`
- `public_api`: `publicApi`
- `framework_root`: `loginHandler`, `authMiddleware`, `listUsers`
- `semantic_dispatch`: `dynamicDispatchTarget`
- `excluded`: `generatedHelper`, `testOnlyHelper`
- `ambiguous`: `ambiguousPropertyTarget`

Notes:

- Express route registrations are modeled as derived framework roots.
- CommonJS and ESM exports are public API evidence, but JavaScript remains
  derived until broader reachability and dynamic-dispatch proof exists.
- Dynamic imports and computed property dispatch are ambiguity evidence, not
  cleanup-safe proof.
