# replay/inputtape — agent scope

## Owned surface

- `go/internal/replay/inputtape/` — the input-tape replay flavor: an
  `http.RoundTripper` that records HTTP traffic to a canonical JSON tape and
  replays it credential-free with no network access.
- This package depends on the `replay` core (for `Canonicalize` and
  `RedactedSentinel`); the dependency arrow points flavor → core, never back.

## Key invariants

- **Replay MUST NOT reach the network.** `RoundTrip` in `ModeReplay` resolves the
  request key against the loaded tape and returns `ErrUnmatchedRequest` on a
  miss. There is no live-transport fallback, and there must never be one — a
  silent network call would defeat the credential-free, deterministic guarantee.
  `TestReplayUnmatchedRequestErrorsLoudly` is a first-class regression case.
- **A recorded tape MUST be credential-free.** Secret headers
  (`Authorization`, `Cookie`, `X-Api-Key`, `X-Amz-Security-Token`, …) and
  secret query params (`token`, `access_token`, `signature`, …) are redacted to
  `<redacted>` before the request is stored AND before the key is computed.
  Over-match is safer than under-match. If you add a provider whose credential
  travels in a non-default header/param, extend `defaultSecretHeaders` /
  `defaultSecretQueryParams` or require the caller to pass it via
  `Config.RedactHeaders` / `RedactQueryParams`.
- **The request key MUST be deterministic and order-independent.** It hashes
  method + path + sorted query + canonicalized body. Do not let header order,
  query order, or JSON key order leak into the key. The body participates in the
  key when present (so two requests differing only in body do not collide).
- **Volatile params are normalized in the KEY only, not in the stored request.**
  A `Config.VolatileQueryParams` entry collapses to `<volatile>` in the key so a
  per-run timestamp matches on replay, but the recorded request keeps the real
  value for human review. Keep secret vs volatile distinct: a secret must never
  be stored; a volatile merely must not break matching.
- **The same Config (redaction + volatile sets) MUST be used at record and
  replay.** Otherwise a request reduces to a different key on each side and a
  recorded interaction silently misses.
- **Concurrency:** `RoundTrip` may be called concurrently. The mutex guards only
  the interactions map and the order slice. NEVER hold the lock across the
  wrapped `transport.RoundTrip` network call. Keep `-race` green.
- **The tape on disk MUST be canonical.** `MarshalTape` sorts interactions by
  `request_key` and round-trips through `replay.Canonicalize` so re-recording
  equivalent traffic does not churn the committed file.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `concurrency-deadlock-rigor` for any change to the record/replay locking or
  the request-capture body handling.
- `eshu-golden-corpus-rigor` if input tapes become inputs to the B-7 golden
  gate (a recorded tape is a fixture the gate could assert on).

## Do not

- Add a network fallback to the replay path.
- Store a credential in the tape, or compute the key from an unredacted secret.
- Hold the mutex across the network round trip in record mode.
- Let header/query/JSON-key order affect the request key.
- Import an unrelated collector package from non-test code (collectors wire the
  tripper; the tripper does not depend on any collector).
