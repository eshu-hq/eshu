# internal/ifa/throughput

The Ifá Layer 3 throughput Odù. It amplifies one base Odù to a named scale-lab
slot and drives the amplified multi-scope corpus through the P2 concurrent
replay driver, so a slot's fan-out actually exercises worker concurrency.

## What it is

`Run(ctx, family, slot, seed, workers)`:

1. amplifies the base Odù via `ifa.AmplifyAtSlot` — one synthetic 1-repo Odù
   becomes an N-scope corpus with disjoint-by-construction payload identities
   (the generic scope_id rewrite the ADR Layer 3 landmine warns against is
   rejected at that seam);
2. writes the amplified cassette to a temp file and loads it through the
   production `cassette.Source` seam;
3. drives `concurrentreplay.Driver{Workers: workers}` into an in-memory
   committer that tallies committed scopes and facts.

It returns the committed scope/fact totals and the driver's reported duration.

## Enforcement classes

Slots carry their enforcement class from `ifa.ScaleSlot` — the same
hermetic/operator-gated split `go/internal/perfcontract` already defines, not a
second perf contract. Smoke and small run hermetically in the `make prove`
common path; medium and above need consistent operator hardware for a meaningful
latency number and are operator-gated. The latency thresholds themselves are
adopted from `specs/scale-lab-corpus.v1.yaml`, not redefined here.

## The hermetic proof

The small-slot run is credential-free: temp-file cassette in, in-memory commit
out, no Postgres/graph/network. Its assertion is on committed counts, not wall
time, so it does not flake on a busy machine. The committed scope and fact totals
are invariant to worker count — the amplified corpus drains completely and
identically at 1, 2, and 4 workers. A worker-count-sensitive total would mean the
driver dropped or double-counted work under concurrency, which is exactly the
race Ifá exists to catch.

## Boundaries

This runner measures drain completeness hermetically. It does not run the reducer
or projector, and it does not assert real backend latency numbers — those are the
operator-gated medium+ slots, driven against a real backend elsewhere.
