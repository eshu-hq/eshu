# Arbiter ruling: #9872 new advisory sources and mirror decision

Date: 2026-11-03. Arbiter: `arbiter-eshu`. Subject:
`docs/internal/design/9872-new-advisory-sources-and-mirror-decision.md` at commit
`8d363f4e66`. The arbiter is read-only; the coordinator recorded this text from
its final message and applied the required changes in the next commit.

RULING: ACCEPT WITH CHANGES. Option B (sources and mirror rules decided here,
Phase 5 gated on item 5.1) stands. Two source rows were not buildable as written
and the cache section contradicted the #603 code.

## Decisions on the two #9872 questions

1. **Mirror mode belongs in this ADR as provisional rules (option B).** Plan
   section 4.7 names new sources and mirror mode as one ADR, and #9872 requires
   the #603 and plan section 4.4 reconciliation. The rules bind nothing until
   item 5.1 reports (Prove-The-Theory-First).
2. **Unreviewed GHSA records: out of scope (default No).** 357,994 unreviewed
   against 37,219 reviewed, auto-published from NVD with no validity check, and
   absent from OSV. If the owner admits them later they are class B or C, never A.

## Required changes (all applied)

1. P1: EUVD kinds collide with the reducer's kind-exact attribution
   (`derived_partition.go`: `known_exploited` is `cisa_kev`, `epss_score` is
   `first_epss`). EUVD emits no `epss_score`. EU KEV needs a `source` field on
   `known_exploited` or an `exploited` field on the `euvd` record; the ADR picks
   one.
2. P1: the CVE List row must not emit `affected_product`, which the reducer
   hardcodes to `nvd`, until the kind carries a source.
3. P2: section 3 misstated #603. Retention is a duration cutoff, artifacts are
   one inline JSON read whole, and cleanup refuses artifacts over 16 MiB. A
   643 MB baseline needs a new artifact version or store.
4. P2: the CVE List class cell omits A (CNA `packageURL` blocks). Amend the
   phases row 4.1 in the same PR.
5. P2: item 4.1 must name its per-CVE endpoint, auth, and owned-CVE derivation.
6. P2: 4.1 depends on 3.1 (#9954); 4.0 need not wait on it.
7. P2: name the reducer edit point, `supportedTrustedAdvisorySourceClass`.
8. P3: GHSA malware records carry GHSA ids; `MAL` ids come through OSV.
9. P3: every source emits `source_snapshot`.
10. P3: list all six cache fields and note the struct gap.
11. P3: the title said "admit four"; Exploit-DB is conditional.

## Owner-question defaults

| # | Question | Ruling on the default |
| --- | --- | --- |
| 1 | Unreviewed GHSA | No. |
| 2 | EPSS attribution | Keep the per-CVE client, no daily CSV. Show a FIRST attribution line pending the owner. |
| 3 | Exploit-DB sign-off | 4.4 does not start; metadata only. #9881 carries a blocked note. |
| 4 | NC-ND OSV opt-in | Mirror opt-in: No. The existing query-driven ingestion of Wolfi and Chainguard records is a disclosed exposure for the owner and legal sign-off, not a coverage regression. |

## Follow-up issue ruling

File 4.0, 4.2, 4.3, and 5.1 after the revised ADR merges, each with its gate in
the body, an existing label, and `Refs #9209`. Items 5.2 to 5.4 are not filed.

Filed after the ADR merged (#9956): #9958 (4.0), #9959 (4.2), #9960 (4.3), and
#9961 (5.1). Each is gated and waits, per the ruling:

- #9958 (4.0) is Phase 4 implementation, outside the session that wrote this
  record. It needs the eshu-sdk release candidate that the owner dispatches.
- #9959 (4.2) and #9960 (4.3) wait on #9954 (item 3.1) and #9958. #9960 also
  waits on the EUVD terms check and the `known_exploited.source` change.
- #9961 (5.1) waits on #9880 (4.1) so the measurement runs the real `cve_list`
  reducer path.
DEFER text: "5.2 to 5.4 implement the shard-scoped generation theory (plan
section 4.4), which item 5.1 has not measured; Prove-The-Theory-First forbids
opening work on an unproven theory. 5.4 also waits on the owner's catalog scope
decision."

## Not checked

Upstream CVE List, EUVD, and raw GitHub rate limits; FIRST copyright and terms
pages; whether a rank table exists beyond `supportedTrustedAdvisorySourceClass`;
the readiness source map's tolerance of unknown sources; the docs build of the
draft.

## Coordinator corrections after the independent review

The independent review (before the PR) found that ruling item 6 rested on a
wrong premise: REJECTED and `replaced_by` are item 1.5 fields and already
shipped, so item 4.1 does not need item 3.1 for them. The ADR keeps 4.1 behind
3.1 for a different reason, serializing the SSVC field after 3.1 on
`vulnerability.cve`. The review also found more reducer, storage, readiness, and
workflow edit points than ruling item 7 named; the ADR lists them and leaves the
full check to item 4.1.
