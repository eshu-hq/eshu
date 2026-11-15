# Arbiter ruling: VulnCheck as an NVD backup (#10720)

Date: 2026-11-15. Refs #10720. Part of #9209. Arbiter: a separate-context
deepest-tier model given the raw measurements in
[10720-vulncheck-nvd-compat-probe.md](../evidence/10720-vulncheck-nvd-compat-probe.md)
and the accepted design in
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
It re-ran no probe and used no credential. This file records its ruling. A
later hostile review corrected two of the arbiter's inputs, which the text below
reflects: the `negate` drop was seen only on the native index, and the reducer
excludes a negated match rather than inverting it. The arbiter's "5 of 5" API
spot-check was in fact 6 of 6.

## Rulings

1. **Backup, rank 3, fresh-edge window class only; not primary.** The order stays
   NVD API, then the official NVD 2.0 feed, then VulnCheck. Primary is rejected:
   NVD is the source of record (owner decision 2026-11-05), raw record equality
   was 13%, 2,184 normalized mismatches are unclassified, the Community tier has
   no SLA, and every reliability figure is one session, one token, one source IP.
   The stuck-window class is already served by the feed's prefix-commit rule, so
   VulnCheck adds nothing there. Feed retirement is recorded as a named hedge in
   the design's sunset-watch follow-up (eshu#10716, noted there by comment); no
   build.
2. **VulnCheck content never enters an NVD-labelled fact.** `vulnStatus` differs
   on 5.1% of overlapping records and on 67% of NVD `Undergoing Analysis`
   records, and the live NVD API sided with NVD on all 6 answered samples.
   Freshly modified records sit in early analysis states, so "fall back to the
   NVD value" is likely to fail where VulnCheck would be used (a hypothesis: the
   mismatch was bulk-clustered by date, not measured at the edge, where 11-14
   showed 0 of 250). A dropped `negate: true` would turn a match the reducer
   excludes into a false affected result; on the compatible endpoint the key was
   present (all `false`) and a `true` was never tested, so this is an untested
   risk, not a finding. The only acceptable shape is
   **ID discovery**: use VulnCheck for CVE id plus `lastModified` (equal on
   11,017 of 11,017 and 60 of 60 samples), and fetch all content from NVD
   `cveId=` reads inside the same window claim, envelope and cursor CAS, with
   overflow as a prefix commit at whole `lastModified` groups.
3. **Implementation is blocked on written vendor answers.** The owner, not an
   agent, sends the ask. Questions: (a) do the Service Terms (revised 2026-04-20)
   bind a Community token on the NVD-compatible endpoint; (b) may CVE ids and
   `lastModified` values, and separately full records, be stored, and for how
   long; (c) may derived findings be shown and exported, and does NVD++
   attribution apply when displayed content is re-fetched from NIST using only
   ids from VulnCheck; (d) the exact attribution form for an API product with no
   UI; (e) does use as ranking or LLM-feature input count as training; (f) is
   the 1,000 requests per minute limit per token or per IP, is there a daily
   quota, and is there any Community freshness commitment.
4. **Defer the implementation issue.** File it only when the vendor's written
   answers are recorded in the evidence note and permit stored id use, and the
   feed-transport work (collector#99) is merged. Title when filed: "NVD window
   claim: VulnCheck ID discovery for the fresh edge"; labels
   `collector:vulnerability`, `type:feature`, `risk:supply-chain`. If the vendor
   declines, close the spike as rejected.
5. **One probe before any build:** a timed NVD `cveId=` walk of the ids VulnCheck
   returns for one live fresh-edge 6 h window, against the 6 s pacer and the claim
   envelope. Single reads ran 3.9 to 21 s with 2 of 8 failing; if that walk cannot
   commit a prefix inside the envelope, id discovery is also dead and the product
   accepts one feed cadence of lag during an API outage.

## Regression plan when filed (failing first)

- R16: a VulnCheck window fixture yields zero facts until NVD content arrives,
  and no emitted envelope field equals a VulnCheck-only value.
- R17: half-open trim of VulnCheck's `[s,e)` against the API's inclusive ends
  (mirror of R1).
- R18: a record that changes between discovery and fetch is upserted once under
  its stable fact key.
- R19: a `transport=vulncheck_ids` label on claim metrics, and the token never
  appears in logs or spans.
- R20: the selector routes to VulnCheck only when the window end is after the
  feed horizon `M1` (design section 1) and the API walk failed; the kill switch forces the API.

## Corrections the arbiter required in the evidence note

The 71 s versus 58 h comparison is struck (58 h is planner cadence, not API
speed); "fresher than the feed" is one Sunday sample, not a cadence; the 59,215
versus 45,757 line is not a coverage check; and the 80% normalized equality is a
floor, so no adapter spec exists.
