# Reducer fact load

## Purpose

`factload` owns how a reducer handler reads the facts for one scope generation,
and what happens when that read fails.

It sits below both the reducer root and the domain-family packages for the same
import-direction reason as `payloadcore` and `factdecode`: the root imports
families to construct their handlers, so a family cannot import the root back,
and every family loads facts (issue #6061, epic #6053).

## Ownership boundary

This package owns:

- `FactLoader` — the port a handler reads facts through. It is defined here
  rather than in `contract` because it names `facts.Envelope`, and `contract` is
  the dependency-neutral vocabulary package.
- `FactKindLoader` and `FactPayloadValueLoader` — optional extensions a store may
  implement to push filtering down. A store that implements neither still
  satisfies `FactLoader`; the loader then falls back to the full `FactLoader`
  contract and returns the whole scope generation unfiltered. Filtering in that
  case happens in the calling domain handler, not here.
- `LoadFactsForKinds` and `LoadFactsForKindAndPayloadValue` — the two scoped
  read shapes, with the push-down and fallback logic.
- `ClassifyFactLoadError` and `retryableFactLoadError` — the retry decision.
- The fact-kind name constants the scoped loader filters on.

It does not own decoding. A loaded envelope whose payload is malformed is
`factdecode`'s problem, not this package's.

## Why the retry classification lives here

A fact load that fails because the store is unreachable must retry; one that
fails because the request was wrong must not. `ClassifyFactLoadError` wraps the
first kind in an error that reports `Retryable() == true`, which the Postgres
queue reads through `errors.As`. Getting this backwards is the difference
between a transient outage costing a retry and it dead-lettering a scope's whole
generation.

## Compatibility

The reducer root keeps `type FactLoader = factload.FactLoader` plus forwarders
in `scoped_fact_loader_compat.go`, so the 64 root call-site files and the
external package naming `reducer.FactLoader` are unchanged. Those forwarders are
transitional and are deleted as their callers move into family subpackages.
