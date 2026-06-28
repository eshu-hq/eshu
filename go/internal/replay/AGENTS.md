# replay — agent scope

## Owned surface

- `go/internal/replay/` — the deterministic replay core: `canonical.go`
  (canonical serialization) and `source.go` (the `Source` interface).
- Replay flavors live in subpackages (`go/internal/replay/cassette/`); each
  subpackage owns its own scope and `AGENTS.md`.

## Key invariants

- **Canonicalization MUST stay idempotent.** `Canonicalize(Canonicalize(x))`
  must equal `Canonicalize(x)` byte-for-byte. This is the load-bearing property
  the whole record/replay workflow depends on; `TestCanonicalizeIsIdempotent` is
  a first-class regression case, never delete or weaken it. Any change to the
  transform, the marshaler, the sort, or the trailing-byte handling must keep
  that test green.
- **The core MUST stay flavor-agnostic.** Every `CanonicalOptions` field is keyed
  by JSON object key name. `replay` MUST NOT import any flavor package
  (`replay/cassette`, future `replay/...`); the dependency arrow points from
  flavor to core, never back. A flavor passes its own keys/sentinels.
- **Numeric fidelity MUST be preserved.** Decode with `json.Number`
  (`dec.UseNumber()`); never let integers or fractional literals round-trip
  through `float64`, or re-records churn the numbers.
- **Volatile sentinels MUST remain valid for their field.** `SentinelObservedAt`
  is a valid RFC3339 instant so a canonicalized cassette still parses as a
  timestamped document. If you add a volatile field whose value has a parse
  contract, its sentinel must satisfy that contract.
- **Secret redaction MUST be by key name at any depth** (over-match is safer than
  under-match for secrets). Do not narrow it to a fixed path without proving no
  secret can hide at a different nesting.
- `Source` MUST stay equivalent to `collector.Source` (it embeds it) so replay
  flavors drop into the live `collector.Service` poll loop unchanged. Do not add
  methods that the existing flavors do not already satisfy without updating them.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-golden-corpus-rigor` because canonicalization defines the recorded
  fixtures the B-7 golden-corpus gate asserts on; a change here can shift every
  cassette's canonical form.
- `eshu-diagnostic-rigor` if you add telemetry or measure throughput.

## Do not

- Import a flavor package from the core.
- Weaken, skip, or delete the idempotency regression test.
- Replace `json.Number` decoding with default `float64` decoding.
- Introduce nondeterminism (map-order-dependent output, `time.Now()`,
  randomness) into the canonical form.
