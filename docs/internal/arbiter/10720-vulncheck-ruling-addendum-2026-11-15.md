# Arbiter ruling addendum: VulnCheck NVD++ as a labelled standing source

Date: 2026-11-15. Refs #10720, #10722, #10671. Part of #9209. Arbiter: a
separate-context deepest-tier model, given the raw measurements in
[10720-vulncheck-nvd-compat-probe.md](../evidence/10720-vulncheck-nvd-compat-probe.md),
the first ruling in
[10720-vulncheck-ruling-2026-11-15.md](10720-vulncheck-ruling-2026-11-15.md),
and the owner's decision below. The coordinator records this file; the arbiter
does not edit files. The arbiter re-ran no probe and used no credential.

This addendum overrides rulings 2, 3, and 4 of the first ruling where they
conflict. The first ruling's reasons for refusing VulnCheck as a primary stay in
force, and its rank for the official NVD feed does not change.

## Owner decision (2026-11-15)

The NVD API fails with chronic 503s, so the owner asked for a second source that
does not depend on it. After the first ruling, the owner read VulnCheck's
published attribution terms for the NVD++ community dataset and decided that the
licence condition for using it in a public or commercial security product is a
prominent label saying the data is VulnCheck NVD++. The owner's reading of that
page is the basis for the rulings below; no vendor wrote to us. The arbiter did
not treat the page as legal advice.

## Rulings

1. **A separately labelled standing source, `vulncheck_nvdpp`.** It replaces the
   first ruling's id-discovery-only shape. It runs as its own lane through the
   same window-claim machinery, with its own cursor row, scope, and counters. It
   never writes an `nvd`-labelled fact and never reads or advances the `nvd`
   cursor.
2. **Rank 7, below `nvd` (6) and `cve_list` (5).** Per CVE, the observation with
   the newer `lastModified` is chosen; a tie goes to `nvd`; the two sources are
   never unioned. The rank is pinned by a test.
3. **Field dispositions.** Keep `id`, `lastModified`, `published`,
   `descriptions`, and `configurations` as VulnCheck sends them, including
   `negate`. Drop the `vc*` fields, any VulnCheck-authored metric or weakness
   entry, zero-valued temporal and environmental scores, and reference tags.
   Restore the `Primary` type on the NVD entry. Carry VulnCheck's status only as
   `vulncheck_vuln_status`, never as `vulnStatus`.
4. **Attribution.** Any API, MCP, UI, export, or doc surface that shows a finding
   whose chosen observation is `vulncheck_nvdpp` says "VulnCheck NVD++". The
   label is absent when the chosen observation is NVD.
5. **Training exclusion.** Facts with this source label are excluded from any
   model training, fine-tuning, or eval corpus, by label.
6. **Operations.** A kill switch stops planning the lane from the next run,
   readiness fails closed when the token is absent, and the token never appears
   in logs or spans. The switch reuses the work in eshu#10716.
7. **The feed transport stays as built.** The NVD feed fallback (collector#99 and
   collector#100) keeps `nvd`-labelled facts flowing and is not replaced by this
   lane.
8. **Precondition before any build.** The compatible endpoint kept `negate` on
   every record seen, but a `true` value was never tested. A reducer that sees a
   dropped `negate: true` would report a false affected result, so one probe must
   fetch a CVE whose NVD record has a `negate: true` node and confirm the flag
   survives. A failure closes the lane.

## Owner-accepted residual risks (2026-11-15)

The owner accepted these on the date above. Neither was answered by a vendor.

- **Storage and modification.** The attribution page says nothing about whether
  VulnCheck records may be stored or modified. We store them and drop fields.
  The page neither permits nor forbids that.
- **Service Terms.** VulnCheck's Service Terms (revised 2026-04-20) may bind a
  Community token and may restrict use as AI training input. Rule 5 keeps the
  data out of training corpora, but the terms were not tested against ranking or
  other model-assisted features.

The owner may send the vendor a courtesy note describing the rank and the label
wording; that is optional and does not gate work.

## Follow-ups

- eshu#10722: the core lane (source token, cursor row, rank 7, attribution
  surfaces, kill switch, telemetry, training exclusion, the `negate` probe).
- eshu-collector-vulnerability-intelligence#103: the collector client,
  normaliser, and envelope for the compatible endpoint.

Each follow-up is worked in the same goal. Neither is deferred.

## Also considered: the cveb.in mirror

The owner asked whether `mirror.cveb.in` helps. Measured 2026-11-15, one session,
one sample:

- It republishes the official NVD 2.0 feed files under the same names
  (`nvdcve-2.0-YYYY.json.gz`, `.zip`, `.meta`). It adds no data the official feeds
  lack.
- Its Modified `.meta` read `lastModifiedDate` 10:00 -05:00 while NIST's read
  12:00 -05:00 at the same moment, so it ran about two hours behind. No cadence is
  published.
- The site names no operator, licence, freshness promise, or SLA, and has no API.

**Ruling: not used.** It could serve only as a second host for the same feed
files, and trusting it would need each file's sha256 checked against the official
`.meta`, which the lag often defeats. Revisit only if `nvd.nist.gov` feed hosting
proves unreliable in operation.
