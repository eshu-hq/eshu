# #11428: bound the supports drain working set

Refs #11428.

The service-catalog vulnerability loader and the security-alert reconciliation
loader drained the supports table through `supplyChainImpactSupportsDrain.drain`,
which appended every active envelope with its full JSON payload into one slice
and stopped only on a short raw page. A hot key grew that slice without bound:
11 OOMKills of the general engine in production, 7.6 GB heap in the live profile
with ~91% under `encoding/json` from drain via page. A 152,000-finding
repository measured about 365 MB through this path.

## The change

`drainPages` streams per-page through a `yield` (one page of envelopes held at
a time) and stops at 100,000 active envelopes with a resume cursor instead of
serving a prefix as complete. Past the cap the drain returns nil envelopes plus
a retryable `SupplyChainImpactSupportsDrainTruncatedError`
(`supply_chain_impact_supports_drain_truncated`, resume from
`fact_id > ResumeCursorFactID` with no loss and no duplication). The class is
deliberately not in the non-counting set: a never-converging hot key spends an
attempt per retry and dead-letters at maxAttempts instead of retrying forever.

## Performance Evidence

Baseline (unbounded drain): working set grows with the hot key with no ceiling;
observed 11 general-engine OOMKills and a 7.6 GB live heap in production, ~91% of
it under `encoding/json` reached from drain via page. A 152,000-finding
repository moves about 365 MB through this path, so a key an order of magnitude
hotter exceeds any engine limit.

After (capped stream, lab Postgres 18 on lab-c1-pg, 127.0.0.1:55432): one drain
holds at most one raw page plus its folded output; the 100,000-active cap bounds
a single drain to hundreds of megabytes by the measured ratio, and a past-cap
key fails fast with a cursor instead of growing. Live drain timings on the lab:
equivalence on adversarial seeds 2.7 s, fence mark landed mid-drain 4.9 s,
concurrent-with-writes 2.0 s. Under the cap the drain serves exactly what the
unbounded drain served (pinned by
`TestSupplyChainImpactSupportsDrainServesUnderCapUnchanged`: fact_id order,
active-only filtering, short-page stop).

## No-Regression Evidence

- RED first: the new bound tests fail to build without the fix (new cap/error/
  stats API); a scratch probe on base symbols showed the old drain serving 1500
  envelopes with a nil error on a 3-full-page hot key.
- `TestSupplyChainImpactSupportsDrainTruncatesOverCapWithResumeCursor`,
  `TestSupplyChainImpactSupportsDrainTruncatesMidPageWithResumeCursor` (mid-page
  cut pins cursor `fact-1199`, unreachable via any other path),
  `TestSupplyChainImpactSupportsDrainServesUnderCapUnchanged`: pass.
- Live against lab Postgres:
  `TestSupplyChainImpactSupportsDrainEqualsFactReadOnAdversarialSeedsLive`,
  `TestSupplyChainImpactSupportsDrainKeepsAMarkLandedDuringTheDrainLive`,
  `TestSupplyChainImpactSupportsDrainConcurrentWithWritesLive`: pass.
- Full `go/internal/storage/postgres` package: green except 4 family-DSN env
  omissions (CI sets per-family proof DSNs), 1 failure proven identical on the
  untouched lane base (`TestResolveDeltaBasesMatchesPerScopeRuleLive`), and 1
  contention hang (`TestStaleGate11234AcceptanceLive`) that passes in isolation.
- `go vet`, `gofmt`: clean. Independent review: ready, zero P0/P1/blocking-P2.

## Observability Evidence

Truncations log `event_name=supply_chain_impact_supports.large_drain` with
`outcome=truncated` (the line prefers the truncated label when both flags are
set), carrying reader, pages, raw/active rows, and duration; the drain stats
(raw rows, active rows, pages, truncated flag) ride the existing sample call,
and the failure class `supply_chain_impact_supports_drain_truncated` names the
dead-lettered hot key. `reads_total` outcomes are unchanged. A dashboard hunting
hot keys watches `outcome=truncated` and the truncation failure class.
