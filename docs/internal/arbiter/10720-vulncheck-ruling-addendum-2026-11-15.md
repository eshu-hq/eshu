# Arbiter ruling addendum: VulnCheck NVD++ as a labelled standing source

Date: 2026-11-15. Refs #10720, #10722, #10671. Part of #9209. Arbiter: a
separate-context deepest-tier model, given the raw measurements in
[10720-vulncheck-nvd-compat-probe.md](../evidence/10720-vulncheck-nvd-compat-probe.md),
the first ruling in
[10720-vulncheck-ruling-2026-11-15.md](10720-vulncheck-ruling-2026-11-15.md),
and the owner's decision below. The coordinator records this file; the arbiter
does not edit files. The arbiter re-ran no probe and used no credential. A later
independent review of this file added the items marked "Review note"; those are
the coordinator's, not the arbiter's.

## What this supersedes

This addendum overrides rulings 1 to 5 of the first ruling where they conflict:
the "backup, fresh-edge only" scope (1), the id-discovery-only shape (2), the
vendor-answers-first block (3), the deferral (4), and the NVD `cveId=` walk
probe (5), which only mattered for id discovery. The regression plan R16 to R20
in the first ruling is replaced by the tests in #10722 and in
eshu-collector-vulnerability-intelligence#103, whose numbering governs. The
first ruling's reasons for refusing VulnCheck as a primary stay in force, and
the order of NVD API, then the official NVD feed, is unchanged. The 10671
limits on a new source value and a new cursor row were written for the NVD
fallback role and do not bind this separately labelled lane.

## Owner decision (2026-11-15)

The NVD API fails with chronic 503s, so the owner asked for a second source that
does not depend on it. After the first ruling, the owner read VulnCheck's
published attribution page for NVD++ and took it to mean that using the data in
a public or commercial security product needs a prominent label saying the data
is VulnCheck NVD++. That is the owner's reading of a public page. No vendor
wrote to us, the page is not legal advice, and it was not confirmed to cover the
output of the NVD-compatible endpoint the lane reads. The rulings below rest on
that reading and on the risks recorded after them.

## Rulings

1. **A separately labelled standing source, `vulncheck_nvdpp`.** It runs as its
   own lane through the same window-claim machinery, with its own cursor row,
   scope, and counters. It never writes an `nvd`-labelled fact and never reads
   or advances the `nvd` cursor.
2. **Rank 7, below `nvd` (6) and `cve_list` (5).** This is the reducer's
   provenance rank (the first ruling's "rank 3" was fallback order). Per CVE,
   the observation with the newer `lastModified` is chosen; a tie goes to `nvd`;
   the two sources are never unioned. The rank is pinned by a test that covers
   both source-priority tables in `provenance_selection.go`.
3. **Field dispositions.** Keep `id`, `lastModified`, `published`,
   `descriptions`, and `configurations` as VulnCheck sends them, including
   `negate`. Drop the `vc*` fields, any VulnCheck-authored metric or weakness
   entry, zero-valued temporal and environmental scores, and reference tags.
   Restore the `Primary` type on the NVD entry. Carry VulnCheck's status only as
   `vulncheck_vuln_status`, never as `vulnStatus`.
4. **Attribution.** Any API, MCP, UI, or export surface that shows a finding
   whose chosen observation is `vulncheck_nvdpp` says "VulnCheck NVD++", and the
   label is absent when NVD is chosen. The data-sources page and the public docs
   list the source in every case, as #10722 requires.
5. **Training exclusion.** Facts with this source label are excluded from any
   model training, fine-tuning, or eval corpus, by label.
6. **Operations.** A kill switch stops planning the lane from the next run,
   readiness fails closed when the token is absent, and the token never appears
   in logs or spans. The switch reuses the work in eshu#10716.
7. **The feed transport stays as built.** The NVD feed fallback (collector#99 and
   collector#100) keeps `nvd`-labelled facts flowing and is not replaced by this
   lane.
8. **Order of work: probe, then build.** The compatible endpoint kept `negate`
   (all `false`) on all 50,216 nodes of the drain sample, but a `true` was never
   tested. A reducer that sees a dropped `negate: true` would report a false
   affected result. The first step of #10722 is one probe: fetch CVEs whose NVD
   record has a negated node, including a nested one, and confirm the flag
   survives. The collector client and the enable switch wait for that result; a
   failure closes the lane.

## Review notes (coordinator, not arbiter rulings)

- **Interaction with the scored selection.** The design makes the product path
  pick a scored observation rather than rank alone (R9, #10717), and says a
  change to the `cve_list` over `nvd` order needs its own ruling. This lane adds
  a third observation to that selection. #10722 must state, with a test, how
  newer-`lastModified` supersession composes with the tier rule, and must not
  move the existing `cve_list` and `nvd` order.
- **Unclassified mismatches.** About 2,184 normalized record mismatches between
  VulnCheck and the NVD feed were never classified, and raw record equality was
  13%. Because ruling 2 lets a fresher VulnCheck record beat NVD for
  `descriptions` and `configurations`, those mismatches can surface. The owner
  has not accepted this risk. Before the lane is enabled in any environment,
  #10722 must classify them or the owner must accept them in writing.

## Owner-accepted residual risks (2026-11-15)

The owner accepted these two on the date above. Neither was answered by a vendor.

- **Storage and modification.** The attribution page says nothing about whether
  VulnCheck records may be stored or modified. We store them and drop fields.
  The page neither permits nor forbids that.
- **Service Terms and AI use.** The Service Terms (revised 2026-04-20) may bind
  a Community token. The evidence note records that they bar training or
  fine-tuning AI models on free offerings without a paid subscription. Ruling 5
  keeps the data out of training corpora; the terms were not tested against
  ranking or other model-assisted features.

## Residual risks the owner has not yet accepted

- **Free or open-licence availability.** The same Service Terms restrict making
  the data available free or under an open licence. This repository is MIT
  licensed, public, and shows findings through API, MCP, UI, and exports.
  Whether API output, SARIF or CSV exports, or committed fixtures count as
  "making the data available" is unanswered. The owner must accept this in
  writing, or send the vendor the question, before the lane is enabled.
- **Endpoint coverage.** Nothing confirms that the NVD-compatible endpoint's
  output is the "NVD++" dataset the attribution page covers.

The owner may send the vendor a courtesy note describing the rank and the label
wording; that is optional and does not gate work.

## Follow-ups

- eshu#10722: the core lane (source token, cursor row, rank 7, attribution
  surfaces, kill switch, telemetry, training exclusion, the `negate` probe, and
  the two review notes above).
- eshu-collector-vulnerability-intelligence#103: the collector client,
  normaliser, and envelope for the compatible endpoint, after the probe passes.

Both are worked in the same goal and neither is deferred; enabling the lane waits
on the probe and on the two unaccepted risks.

## Also considered: the cveb.in mirror

The owner asked whether `mirror.cveb.in` helps. The existing rulings already
reject a third-party NVD-shaped mirror as a source
([10671 ruling](10671-cvelist-mirror-ruling-2026-11-15.md), "What will not be
done", item 2, and the design's option C). Measured 2026-11-15, one session, no
raw record kept:

- It republishes the official NVD 2.0 feed files under the same names
  (`nvdcve-2.0-YYYY.json.gz`, `.zip`, `.meta`) and adds no data the official
  feeds lack.
- Its Modified file was about two hours behind NIST's at the same moment (10:00
  against 12:00 -05:00 in the `.meta`; the HTTP `Last-Modified` headers differed
  by the same two hours at a second reading). No cadence is published.
- The site names no operator, licence, freshness promise, or SLA, and has no API.

**Ruling: not used,** consistent with the earlier rejection. Revisit only if
`nvd.nist.gov` feed hosting proves unreliable in operation, and then only with
every file's sha256 checked against the official `.meta`.
