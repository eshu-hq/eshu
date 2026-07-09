# AGENTS.md — internal/secretcrypto guidance for LLM assistants

## Read first

1. `go/internal/secretcrypto/README.md` — package contract, envelope format,
   AAD schemes, and fail-closed contract.
2. `go/internal/secretcrypto/doc.go` — godoc summary.
3. `go/internal/secretcrypto/keyring.go` — `Keyring`, `Seal`, `Open`,
   envelope encode/decode.
4. `go/internal/secretcrypto/env.go` — `KeyringFromEnv` DEK loader.
5. `go/internal/secretcrypto/keyring_test.go`, `env_test.go` — required
   security-property tests; extend the matching table when you add a case.

## Invariants this package enforces

- **Fail closed on decrypt** — `Open` returns only `ErrDecrypt` for every
  failure mode (unknown `key_id`, tag mismatch, truncation, malformed
  structure, AAD mismatch). Never add a distinct error, log line, or return
  value that would let a caller (or an attacker probing the API) learn which
  specific check failed.
- **No partial plaintext** — `Open` must never return non-nil bytes alongside
  a non-nil error, and must never return bytes that have not passed GCM
  authentication.
- **Fresh nonce per seal** — every `Seal` call generates a new 12-byte
  `crypto/rand` nonce. Never derive a nonce from the plaintext, key, or a
  counter; AES-GCM catastrophically fails if a (key, nonce) pair repeats.
- **No ephemeral DEK** — `KeyringFromEnv` must never invent a key when
  `ESHU_AUTH_SECRET_ENC_KEY(_FILE)` is unset. Returning `ErrKeyNotConfigured`
  is correct; generating a random key is not, because it would silently make
  every already-sealed envelope unrecoverable.
- **`KeyID` cannot contain `.`** — the envelope format splits on `.`;
  `NewKeyring` must keep rejecting ids that would make that split ambiguous.
- **Immutable, defensively copied keys** — `NewKeyring` copies key material;
  do not let a `Keyring` alias caller-owned byte slices, and do not add
  mutation methods.
- **Concurrency safety** — `Keyring` is read-only after construction and
  used from concurrent callers (API/reducer paths in #4963/#4966 will call
  `Seal`/`Open` from multiple goroutines). Do not add unsynchronized mutable
  state to `Keyring`.

## Boundaries

- This package is a pure library: no database, HTTP, CLI, or telemetry
  wiring. Startup wiring (deciding whether an absent DEK is fatal, emitting
  `eshu_dp_auth_secret_seal_total`/`..._open_total`, etc.) belongs to
  #4963/#4966, not here.
- AAD construction is entirely the caller's responsibility. This package
  only binds whatever bytes it is given; it does not know about tenants,
  workspaces, or provider config ids. Do not add AAD-building helpers here
  that encode knowledge of a specific caller's schema — that traps a schema
  change into a shared package two unrelated callers depend on.
- Do not add a KMS-backed key resolver in this PR's scope. The `KeyID ->
  []byte` indirection in `Keyring` is the seam for one; building the
  resolver is future work.

## Common changes and how to scope them

- **Add a key-sourcing precedence rule** — extend `resolveKeyMaterial` in
  `env.go` and add a case to `env_test.go`. Keep the "file wins" precedence
  when adding new sources; do not silently fall back to a weaker source on
  a read error.
- **Change the envelope format** — treat this as a breaking compatibility
  change. Existing sealed rows depend on the exact `ESK1.<key_id>.<nonce>.
  <ciphertext>` shape; bump the scheme tag (`ESK2`) rather than changing
  `ESK1`'s meaning, and keep `Open` able to read whichever tags are still in
  use.
- **Add a rotation helper** — keep it in this package only if it operates
  purely on `Keyring`/`KeyID` types. Anything that reads or writes rows
  (re-sealing an old envelope under a new primary) belongs in the caller
  that owns the table.

## Anti-patterns specific to this package

- Any code path in `Open` that returns plaintext before GCM authentication
  succeeds.
- Reusing a nonce, or generating one with `math/rand` instead of
  `crypto/rand`.
- Auto-generating a DEK when none is configured.
- Logging, printing, or including raw key material, plaintext secrets, or
  full envelopes in error messages, tests with real-looking fixtures, spans,
  or metrics. Test keys must be synthetic byte patterns, never anything that
  looks like a real deployment secret.
- Wrapping `ErrDecrypt` with `%w` in a way that changes its identity (callers
  rely on `errors.Is(err, secretcrypto.ErrDecrypt)`).
