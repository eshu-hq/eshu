# cabal Parser

Repository-side `.cabal` manifest parsing for the Others Haskell lane
(#9308). Extracts the `tested-with` compiler specs as guarded
`cabal_runtime_evidence` for the runtime declaration emitter. No other field
is read: sibling fields can carry userinfo URLs, so the registry binding is
metadata-only and this package keeps no body.

## Bounds

Column-zero `tested-with` only; continuations extend it. Indented
`tested-with`, conditionals in scope, duplicate or empty fields, and NUL
bytes fail closed. Every entry passes `shared.SafeVersionSpec`.
