# TSX Dead-Code Fixture

Expected symbols:

- `unused`: `UnusedPanel`
- `direct_reference`: `formatTitle`
- `entrypoint`: `Page`
- `public_api`: `ProfileCard`
- `framework_root`: `POST`
- `semantic_dispatch`: `LazyWidget`
- `excluded`: `GeneratedPanel`, `TestOnlyPanel`
- `ambiguous`: `useDynamicHook`

Notes:

- Next.js `route.tsx` exports are modeled with the shared JavaScript-family
  route root logic for dead-code protection; exact route handler bindings are
  emitted separately through parser `route_entries` when the handler is proven.
- React component exports, hooks, lazy imports, and component maps remain
  derived or ambiguous evidence until graph reachability proves exactness.
