# replay

The shared core of Eshu's deterministic replay framework (epic #4102, R-1):
the **canonical serialization** every recorder and validator depends on, and the
`Source` interface every replay flavor implements.

## Why canonicalization is load-bearing

A recorder that wrote raw live collector output would churn the entire fixture on
every refresh: `observed_at` timestamps move, `generation_id` is freshly minted
each run, and Go map iteration order shuffles object keys and fact lists. A diff
full of incidental churn is unreviewable, which defeats the point of recording a
fixture at all.

`Canonicalize` removes that churn so a recorded fixture is stable, reviewable,
and **byte-identical** when re-derived from equivalent input. It is the property
the record/replay workflow is built on, so it is the property R-1 proves first
(`TestCanonicalizeIsIdempotent`).

## What the canonical form guarantees

Given a JSON document and a `CanonicalOptions`, `Canonicalize`:

1. **Sorts every object key.** The document is decoded to `map[string]any` and
   re-encoded, so `encoding/json` emits keys in sorted order at every depth.
2. **Collapses volatile fields to fixed sentinels.** `observed_at` →
   `SentinelObservedAt` (a valid RFC3339 instant so the fixture still parses as a
   timestamped document). Configurable per flavor via
   `CanonicalOptions.VolatileKeys`.
3. **Normalizes run-specific unique ids deterministically.** `generation_id` is
   not collapsed to one constant — it is the `scope_generations` primary key, so
   a single sentinel would collide multiple scopes. Instead each scope's
   `generation_id` is derived as `canonical-generation-<hash(scope_id)>`: stable
   across re-records (the seed is the stable `scope_id`, not the run-specific
   value) yet unique per scope. Configurable via `CanonicalOptions.DerivedKeys`.
3. **Stably orders recorded arrays.** `scopes` by `scope_id`, `facts` by
   `stable_fact_key`, with the element's canonical bytes as a total-order
   tiebreaker. Configurable via `CanonicalOptions.SortArrays`.
4. **Redacts configured secret keys** wherever they appear in the tree (matching
   is by key name at any depth, so a secret cannot leak by being nested
   differently than expected). Configure with `WithRedactedKeys`.
5. **Preserves numeric fidelity.** Decoding uses `json.Number`, so integer and
   fractional literals are never rewritten through a `float64` round-trip.
6. **Is idempotent.** `Canonicalize(Canonicalize(x)) == Canonicalize(x)`.

The core is **flavor-agnostic**: every option is keyed by JSON object key name, so
`replay` never imports a flavor. `DefaultCanonicalOptions` encodes the
fact-envelope (cassette) defaults; a parser-fixture or other flavor passes its
own keys.

## The `Source` interface

`Source` embeds `collector.Source`. Every replay flavor that feeds the collector
boundary — the cassette reader in `replay/cassette` today, parser-fixture and
other envelope flavors later — yields one `collector.CollectedGeneration` per
recorded scope, credential-free, and so drops into the same `collector.Service`
poll loop the live collector uses. Flavors that record at a different seam (for
example the input tape, which records HTTP responses via an `http.RoundTripper`)
are not `Source`s and live in their own packages under `replay`.

## Layout

```
replay/
  canonical.go        canonical serialization core
  source.go           the Source interface
  cassette/           credential-free cassette replay flavor (re-homed from collector/)
```

## No-Regression Evidence

`Canonicalize` performs no I/O and holds no shared state; it is a pure transform
over a decoded JSON tree. Verified by `go test ./internal/replay/... -count=1`.
The re-home of `collector/cassette` → `replay/cassette` is import-path-only: the
package name, types, and behavior are unchanged, proven by the unmodified
`cassette` test suite passing at the new path.
