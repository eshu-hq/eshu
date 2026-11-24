# Supply-Chain Active Evidence Signals

A supply-chain impact pass loads its evidence by resolving matching fact IDs
from a fixed set of index-anchored statements, then fetching the facts by
primary key in 500-ID pages (#8880). Each statement has a stable name, so
these signals say which one a slow or surprising load spent its time in.

The `branch` label is a closed set: one name per statement in the
supply-chain impact branch catalog (for example `vulnerability_cve_id`,
`package_id`, `file_repo_id`, `vulnerability_suppression`,
`foreign_key_guard`), plus `scope_resolution` (repository IDs to active
scopes), `payload_page` (one primary-key page), `identity` (one
canonical image-identity page), and `file_fold` (one byte-ordered page of
parser file facts folded into the reachability index and dropped, #9781). Statements that a pass's filter does not need
are not issued and record nothing. `vulnerability_upstream_cve_id` runs once per
120 ids of its list and records each statement, so its histogram count is
statements, not passes. If one of them climbs from milliseconds into seconds, the
cve key probe's custom plan may have moved off its index: compare with
`vulnerability_cve_id`, which runs under the same bound.

- `eshu_dp_supply_chain_impact_active_evidence_branch_seconds` — histogram
  of one statement's wall time, labeled `branch`. A branch whose latency
  climbs while its row count holds names the index that stopped serving it.
- `eshu_dp_supply_chain_impact_active_evidence_branch_rows_total` — counter
  of rows one statement returned, labeled `branch`.
- `eshu_dp_supply_chain_impact_active_evidence_payload_bytes` — histogram
  (unit `By`) of the raw payload bytes one statement read, labeled `branch`
  (`payload_page`, `identity`, or `file_fold`). `payload_page` and `identity`
  bytes are kept decoded for the pass; `file_fold` bytes are decoded one row at
  a time and dropped. A `file_fold` tail that climbs while `payload_page` holds
  is a larger JS/TS corpus the fold is absorbing at no heap cost. A
  `payload_page` tail that climbs is retained memory growing toward the payload
  budget below.
- `eshu_dp_supply_chain_impact_active_evidence_foreign_key_guard_rows_total`
  — counter of rows the foreign-key guard returned. The guard holds only rows
  whose payload carries an identity key their kind's statements do not probe,
  so it should stay at zero. A nonzero rate means a collector or reducer
  started writing a new key on a kind; the WARN line `supply chain impact
  foreign-key guard returned rows` names the kinds. Results stay correct;
  the guard just pays for them.

The reducer's `reducer execution succeeded` line for `supply_chain_impact`
carries the same accounting summed over the pass:
`sub_signal_active_evidence_branch_<branch>_rows`,
`sub_signal_active_evidence_branch_<branch>_statements`,
`sub_duration_active_evidence_branch_<branch>_seconds`, and the
`active_evidence_loads`, `active_evidence_resolved_fact_ids`,
`active_evidence_payload_pages`, `active_evidence_guard_rows`, and
`active_evidence_truncated_loads` sub-signals, plus the pass totals
`active_evidence_files_folded`, `active_evidence_payload_bytes_folded`, and
`active_evidence_payload_bytes_retained` (#9781). Read them next to
`sub_duration_load_active_evidence_seconds`.

`vulnerability_cve_id` runs one statement per 120 ids (#9994), and the histogram and
the sub-duration count each statement. A pass with 1,000 CVE ids reports 9 statements
on that branch, so its summed seconds cover 9 executions. A slow statement on this
branch that plans on `fact_records_vulnerability_active_advisory_lookup_v2_idx` is
the walk this bound exists to avoid. Check in this order:

1. Read `MaxCVEIDsPerStatement` in the source of the running version. Above 171 it can
   walk at QA's statistics.
2. If the bound is intact, the statistics may have moved. An `ANALYZE` can change the
   correlation stored for the advisory index's expression, and a large enough value walks
   inside the bound. Only its size counts, because the plan cost uses its square. Read it
   with
   `SELECT correlation FROM pg_stats WHERE tablename = 'fact_records_vulnerability_active_advisory_lookup_v2_idx' AND attname = 'expr'`.
   QA measured -0.6.
3. Run `EXPLAIN` on one chunk of the same length with `plan_cache_mode` set to
   `force_custom_plan`, then see [#10050](https://github.com/eshu-hq/eshu/issues/10050)
   for the fix to the estimate.

## Evidence headroom

A pass may charge 100,000 expansion envelopes and 256 MiB of retained raw
payload to its evidence budget. Above either it goes partial: it upserts but
retracts nothing, so a scope that crosses a limit on every pass keeps its stale
findings (#7154). Both limits report under the `evidence_budget` cause. The
WARN line `supply chain impact evidence truncated; findings not retracted` carries
`budget_envelopes` with `budget_envelopes_limit` and `budget_payload_bytes` with
`budget_payload_bytes_limit`; the charge that is over its limit is the reason.
The byte limit is charged page by page during the load, so the first round of a
pass, which carries the heaviest statements, stops at the page that crosses it.
The finding's missing-evidence note, `active supply-chain evidence expansion
limit reached`, covers either limit.

JS/TS parser `file` facts that the active-evidence file branches resolve, and the
Python evidence those rows carry (decorators), are folded into the reachability
index as they are scanned and are not retained, so they charge neither limit
(#9781). Before that change a pass kept each file's decoded payload, about 140 KB
of heap per file, and the envelope count alone could not bound it. A pass that
went partial only because of file count now completes. The standalone Python
reachability stage is unchanged: it still loads all-language `file` facts whole,
and they charge neither limit. It is the next memory risk (#9860).
The payload limit charges the raw bytes of every payload a load returns, including rows the pass later drops as duplicates of ones it already holds, so it can read higher than what the pass finally keeps. That errs toward an earlier, visible partial pass, never a silent drop. The
decoded form of a JSON payload is roughly 3 to 5 times its raw size, so 256 MiB
raw bounds a pass to roughly 0.8 to 1.3 GiB of heap. Two gauges show the
climb before the limit and the scopes already past it (#9778):

| Metric | Type | Key labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_partial_scopes` | observable gauge | `cause` (`active_expansion_rounds`, `evidence_budget`, `other`) | Scopes whose last recorded pass on this process ended partial. Once the process has recorded a pass, every cause reports, zero included, so none stuck reads 0. Before that there is no series, so a binary that never runs the domain exports no constant 0. A later complete pass of the scope releases it. A suppression-tail truncation is not counted: it does not stop retraction. |
| `eshu_dp_supply_chain_impact_evidence_budget_ratio` | observable gauge | none | The widest pass in the last hour, as envelopes charged to the budget over the budget. 1 means the budget is spent; above 1 a pass crossed it, because the load that crosses the limit is kept. No series until a pass has run in the window. |

The ratio counts only what the budget charged. A protected-scan pass also holds
derived OS-package, analysis, and advisory facts it adds after the static load;
those are not charged, cannot make the pass partial, and are not in the ratio
(they are in `sub_signal_evidence_expansion_envelopes`).

Both gauges are process-local and per replica, and add no Postgres query. Read
them as a per-replica view, not a global count, and not as a bound in either
direction:

- A scope that replica A last saw partial and replica B later completed stays
  partial on A until A next handles it, which overstates A.
- A restart empties the gauge until each scope is handled again, which
  understates it.
- A scope two replicas both saw partial counts once on each, so summing
  replicas can count it twice, and a scope partial for two causes counts under
  both.
- The tracker keeps the pass that recorded last for a scope. Two admitted
  passes of one scope can record out of order, so the gauge can show the older
  pass until the scope's next pass.

The WARN line `supply chain impact evidence truncated; findings not retracted`
names the scope and is the exact source.

The success line also carries `sub_signal_evidence_partial` (1 when the pass
did not retract), `sub_signal_evidence_budget_ratio`, and
`sub_signal_evidence_payload_bytes_ratio` (retained raw payload bytes over the
payload limit), so a log search can count partial passes directly.

`sub_signal_evidence_envelopes` and the `evidence_envelopes` field of the WARN
line count each parser file the fold absorbed as one envelope, so they keep the
meaning they had before the fold: how many evidence envelopes the pass loaded.
The charge against the envelope budget does not include folded files (see
`sub_signal_active_evidence_files_folded`), so on a file-heavy pass the count
can exceed the budget without the pass being partial.

## OS advisory joins

An OS distribution advisory (Debian, Ubuntu, Alpine, AlmaLinux, Rocky Linux)
becomes a finding only
when its package key meets an installed `os_package` row from an image scan.
A loaded advisory that meets no row produces nothing, so a broken join looks
the same as a fleet with nothing affected. Before #9848 that is what happened:
every OSV advisory with a versionless, qualified purl (all Debian and Alpine
records, and many Ubuntu ones) missed, because that purl never equalled the
installed row's key; AlmaLinux and Rocky Linux advisories missed a second way
(#9844), because their upstream purl namespaces never equalled the installed
row's canonical vendor key. One counter separates loaded from joined:

| Metric | Type | Key labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_os_advisory_joins_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `alma`, `rocky`, `other`), `outcome` (`joined`, `not_joined`) | One increment per OS advisory package entry per index the pass evaluates: the fleet-wide index once, and on a protection-engaged pass each authorized scan target's index once, because the scanner worker's derived `os_package` rows exist only there. An advisory present in several indexes is counted in each. `joined` means at least one installed row of that vendor and package met it in that index; the counter still increments once per advisory entry per index. One joined entry on the fleet-wide pass can now produce one finding per image subject digest and installed version (#9858, #10057), and one per release line and installed version for rows with no digest (#9984), so finding counts can exceed joined counts when images or lines disagree. A vendor whose `not_joined` rate climbs while `joined` stays at zero, on a fleet that runs that vendor's images, means the advisory key and the installed key have drifted apart again. |
| `eshu_dp_supply_chain_impact_os_advisory_release_selections_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `alma`, `rocky`, `other`), `result` (`release_matched`, `release_unlisted`, `release_untagged`, `host_release_unknown`, `vendor_source`, `vendor_line_mismatch`, `vendor_line_unknown`) | One increment per joined OS advisory package entry, on the same indexes as the join counter. It records the strongest of what the entry's range release tags say about each joined row's release line (#10056), so the label never depends on which row the index meets first. A finding's own row can still read weaker than the label: read its `match_reason` for the verdict on its own row. `release_matched` means the verdict and the fix came from a joined host's own release. `release_unlisted` means the advisory names other releases but no joined host's. `release_untagged` means a range carried no tag, so the untagged union rules applied. `host_release_unknown` means no joined row's `distro_version` mapped to a release line. A vendor-sourced fact (the source is the row's vendor advisory source) is checked against the row's line through its purl `distro` qualifier (#9945): `vendor_source` means the qualifier names a joined row's line, so the fact decides that host as published; `vendor_line_mismatch` means it names no joined row's line, so the fact is excluded from the exact and known-fixed verdicts and holds each joined host at the weak `os_release_not_in_advisory` signal at most; `vendor_line_unknown` means no line could be compared on any joined row (no qualifier, a codename or a vendor the line table does not cover, conflicting qualifiers, or unknown host lines), so the fact carries no release evidence: it never yields `affected_exact` or `not_affected_known_fixed`, its fixed versions are withheld, and a list or range that names the host reads `possibly_affected` / `os_release_unverified`. A non-OSV OS fact whose source is not the installed row's vendor takes the same arm, even beside a verified vendor fact, and so does an rpm exact-version list: the line table has no rpm row, so no rpm fact earns a verified line and none reads `affected_exact`. The same holds on the authorized-scan pass and for an SBOM component with no installed row, where no host line exists to verify against. A `vendor_line_unknown` share points at a producer purl shape the reader does not cover. Tagged facts report `release_matched`/`release_unlisted` (or `host_release_unknown`); `release_untagged` means a range carried no tag: that is expected, not a defect, and it changes no verdict. After activation, a `release_untagged` share that stays high after an OSV resync means the converter tag or the factschema decode is missing. The reducer success log carries the same split as the `os_advisories_release_matched`, `os_advisories_release_unlisted`, `os_advisories_release_untagged`, `os_advisories_vendor_line_mismatch`, and `os_advisories_vendor_line_unknown` sub-signals, for an operator without a metrics backend. A tagged fact whose host line is unlisted reads `possibly_affected` / `os_release_not_in_advisory`, never known fixed, never exact; read that from the finding's `match_reason`. A `host_release_unknown` share means a producer `distro_version` shape the rule does not cover. |
| `eshu_dp_supply_chain_impact_os_advisory_upstream_groups_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `alma`, `rocky`, `other`), `result` (`grouped`, `ungrouped`) | One increment per `vulnerability.cve` record the OS advisory grouping pass examined: a record whose id has an OS vendor prefix or that carries an `upstream` list, counted once per pass over the fleet-wide index only. The per-target indexes of the authorized derived pass apply the same grouping but do not count here. `grouped` means the record's `upstream` list named at least one CVE id other than the record's own key, so the record and its package facts are also indexed under that CVE and one installed package reads one finding per CVE. `ungrouped` means an OS vendor record with no usable CVE key keeps its own group. The label comes from the record id prefix only. The reducer success log carries the same split as the `os_advisories_grouped` and `os_advisories_ungrouped` sub-signals. A source that reads only `ungrouped` means the producer's `upstream` lists are not reaching the reducer: the OSV collector predates the field, or the stored records were collected before it. Records already stored have no `upstream` until they are collected again. |

Registry advisories (npm, PyPI, Go, and so on) are not counted. An advisory
that meets no row is normal for a package the fleet does not install, so read
the ratio per vendor, not the raw `not_joined` count.

The success line carries the same totals for one pass as the
`os_advisories_considered` and `os_advisories_joined` sub-signals, written even
when zero. `os_advisories_considered` above zero with `os_advisories_joined` at
zero is the "loaded, none joined" shape. `os_advisories_joined_via_source`
counts the joined findings that came through an installed row's source package
name rather than the binary name (#9859); it stays zero on a fleet whose
advisories all name installed binaries.

| Metric | Type | Key labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_os_release_not_in_advisory_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `alma`, `rocky`, `other`) | One increment per finding whose advisory carries release tags but none for the installed release (#9806). A climbing rate on a steady corpus means advisories stop listing the releases the fleet runs. |

The success line also carries the `os_release_not_in_advisory` sub-signal,
written even when zero.

A repository that installs several versions of one package gets one finding per
version (#11113). The success line carries the `multi_version_repository_groups`
sub-signal, written even when zero: the number of (repository, CVE, advisory,
package) groups whose final findings hold two or more observed versions,
provider security-alert findings included (#11121). It is a
per-pass count with no metric, label, or CVE id. A holder that reads zero while
its repositories hold several lockfile versions of an affected package means the
per-version split is not running on that path.

Several sources can publish one package id for one CVE, and one fact names the
finding. These signals say what that fact joined (#9990).

| Metric | Type | Key labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_os_representative_joins_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `alma`, `rocky`, `other`: the fact that named the group), `outcome` (`joined`, `reselected`, `other_anchor`, `no_joinable_representative`, `no_installed_row`) | One increment per OS-shaped (CVE, package id) group, once per index the pass evaluates, so a group on a protection-engaged pass counts once on the fleet-wide index and once on each scan target's. `joined` means the fact source order picked joined an installed row, including a blank-ecosystem group narrowed to the facts that join. `reselected` means source order picked a fact with no anchor and another fact of the group joined the row and named the finding. `other_anchor` means no row joined, but an SBOM subject or a consuming repository anchors the finding. `no_joinable_representative` means installed rows sit under the group's keys and no fact of the group passes the join's gates, so no finding is written: usually no fact comes from those rows' vendor, a vendor fact also fails on an ecosystem or purl that does not meet the row, and no fact joins a row from a non-vendor repository. `no_installed_row` means no row in that index sits under the group's keys, which is normal when no scanned host holds the package; an `os://` package id with no purl also lands here, because rows are keyed by purl. |

The success line carries the same split as the `os_representatives_joined`,
`os_representatives_reselected`, `os_representatives_other_anchor`,
`os_representatives_no_joinable`, and `os_representatives_no_installed_row`
sub-signals, written even when zero. A rising `no_joinable_representative`
points at facts that do not meet the installed rows, so read the group's facts and
the rows' repository class first. `reselected` above zero is expected once
a vendor producer and another source publish the same package id.

## Criteria expansion bound

A widely shared product-criteria value can make the until-stable evidence
load walk every vulnerability that names it, stopping at the round limit and
leaving the scope partial (#11087). The provenance bound keeps only two
output-relevant criteria hops: component-origin criteria (components the pass owns:
its own source, or a document attached to a digest its source holds) drive both the product and component probes, and seed-CVE product-origin criteria drive only
the component probe. All other derived criteria are suppressed at derivation
and counted. The success line carries the pass accounting as sub-signals,
written even when zero so bound-on and bound-off passes stay comparable:

- `criteria_bound_suppressed_transitive` — distinct transitive criteria
  suppressed, summed over rounds. Judge success on loads and pass-state
  conversion, never on this count: a pure-seed scope correctly reads 0.
- `criteria_bound_loads` — until-stable loads the pass ran.
- `criteria_bound_component_criteria` — criteria on the both-probes arm.
- `criteria_bound_seed_criteria` — seed-CVE criteria on the component-only arm.
- `criteria_bound_seed_cves` — seed CVE count after alias/upstream closure.

## Round-cap frontier

The until-stable load stops after 8 rounds. A pass the cap stopped is
partial: it upserts but retracts nothing (see `active_evidence_truncated`).
The cap is a depth cap on a graph walk, so its count alone does not say which
arm of the follow-up filter keeps the walk going or how much was left
(#11156). A pass that stopped at the cap reports that frontier, taken from the
follow-up filter that was still non-empty after round 8, after the provenance
bound and the scope restrict, so it is what round 9 would have asked for.

The result line carries these sub-signals, written for a pass the round cap
stopped and absent on every other pass (a budget cut, a manifest refusal, a
converged pass):

- `rounds_cut_rounds` — the cap the pass stopped at (8).
- `rounds_cut_envelopes` — envelopes the pass held at the cut: the scope load,
  the repository and manifest loads, and the active-evidence rounds, before
  the later expansion stages. This is not the budget charge: read
  `evidence_budget_ratio` for that.
- `rounds_cut_frontier_<arm>` — values the arm still held, zero included. The
  arm set is closed and equals the follow-up filter: `package_ids`, `purls`,
  `cve_ids`, `upstream_cve_ids`, `alias_ids`, `advisory_ids`,
  `subject_digests`, `document_ids`, `product_criteria`,
  `seed_product_criteria`, `repository_ids`, `file_repository_ids`,
  `image_refs`.

The same pass writes one INFO line, `supply chain impact evidence expansion
cut at the round cap`, after the `evidence truncated; findings not retracted`
WARN line. It carries `scope_id`, `generation_id`, `intent_id`, `pass_mode`
(`full`, `scoped`, and the fallback modes of
`eshu_dp_supply_chain_impact_pass_total`), `cause`
(`active_expansion_rounds`), `rounds`, `cut_envelopes`, and one
`frontier_<arm>` field per arm. There is no new metric: ids are log fields
only and no arm name or id is a metric label. The count of cut passes stays
`eshu_dp_supply_chain_impact_evidence_truncated_total{reason="active_expansion_rounds"}`.

Read the line in this order:

1. `frontier_*` with one large arm (for example `package_ids` or
   `repository_ids`): that arm is what the walk keeps reaching. Several arms
   open at once points at a walk across package, CVE and repository.
2. The same scope across passes. A scope whose line repeats is one that never
   retracts (#11156).
3. `cut_envelopes` against the budget in the WARN line, to tell a wide scope
   from a deep one.

The diagnostic changes nothing the pass does. A test runs a seeded pass cut at
round 8 with and without the diagnostic read and compares the writer input
byte for byte against what the code wrote before the diagnostic existed.

## Re-pend cause and pass outcome

`eshu_dp_supply_chain_impact_pass_outcome_total` (#10972) counts each findings
pass that committed its write, with the reason the pass ran and whether it
changed anything. It exists to answer one question in the QA environment: of
the passes the queue re-runs, how many ran again for nothing?

It fires after the writer commit and before the holder fan-out, because whether
a pass changed output is known only then. A pass whose write fails records
nothing here; `eshu_dp_supply_chain_impact_pass_total` still counts it at its
evidence load, so the two can differ by the failed passes.

| Label | Values | Meaning |
| --- | --- | --- |
| `domain` | `supply_chain_impact` | Reducer domain. |
| `mode` | `full`, `scoped`, `scoped_fallback_sentinel`, `scoped_fallback_out_of_stamp` | How the pass ran, as on `pass_total`. |
| `pass_reason` | `scoped`, `no_stamp`, `sentinel`, `no_seed`, `out_of_stamp`, `engaged_protection` | Why the pass ran scoped or full. |
| `repend_cause` | `first`, `retry`, `drain`, `fanout_rerun`, `other_repend`, `unknown` | Why the pass was claimed. |
| `changed` | `changed`, `unchanged` | `changed` when the commit changed or retracted a finding row, changed a suppression, or touched an advisory group. |
| `change_class` | `none`, `unknown` | `none` on an unchanged pass; `unknown` on a changed one. |

The pass derives `repend_cause` from what the claim already loaded, with no new
query. The checks run in this order:

1. `retry`: the attempt count is above one.
2. `drain`: the item carries a `fanout_remaining` holder list.
3. `fanout_rerun`: the item carries a `fanout_origin` stamp, the attempt count
   is one, and no owed marker is set. A reopen of a succeeded item and the
   replay of an item that was in flight when the stamp landed leave identical
   payloads, so this value covers both. Splitting them needs the reopen
   statement to record the prior status, which is a separate change.
4. `other_repend`: the item's cycle start is later than its enqueue time, so
   something re-pended it without a stamp (a completion fan-out, an operator
   replay, or a re-pend of a pass that carried an owed marker).
5. `first`: the cycle start equals the enqueue time.
6. `unknown`: the claim carried no usable timestamps.

`change_class` stays `unknown` for every changed pass. Separating a change that
touched only the EPSS and KEV derived fields (`epss_probability`,
`epss_percentile`, `priority_contributions`,
`provenance.selected_epss_score_date`) from any other change needs the stored
row's fields, and the writer's digest read returns only the digest. Reading the
fields would add to the hot path, so the split is left for a change that
measures that cost.

`pass_reason` splits the `full` mode that `pass_total` lumps together. The
checks run in this order:

1. `scoped`: the load scoped to the stamp.
2. `out_of_stamp`: the pass qualified to scope but the holder carries active
   finding rows outside the stamp (#10415), so it ran full.
3. `sentinel`: a one-hop stamp that names an unbounded change.
4. `no_stamp`: nothing bounded to scope by: a genuine pass, an unstamped or
   empty stamp, or a retry.
5. `engaged_protection`: a bounded stamp, but the protected or static-only
   evidence path never scopes. The QA environment does not run it; other
   stores may.
6. `no_seed`: a bounded stamp that nothing seeded: no finding row and no local
   stamped advisory, a loader without the seed seam, or a failed seed read
   (that case also logs `supply chain impact scoped seed read failed`).

The same fields ride on one INFO line per pass, `supply chain impact pass
complete`, with `scope_id`, `generation_id`, `intent_id`, `attempt_count` and
the pass `mode`, and as attributes `repend_cause`, `changed` and `change_class`
on the span that wraps the pass (`reducer.run`). Ids are log fields only; no id
is a metric label, and the series count is at most 4 modes x 6 causes x 2 x 2.

Read it as a rate. The share of `changed="unchanged"` among
`repend_cause="fanout_rerun"` is the useless re-run share of the fan-out loop;
the same share among `drain` shows how much of the self-drain feeds it. Query
the 3-hour QA window, not a single reading.

## Fan-out time by site

`eshu_dp_supply_chain_impact_fanout_site_duration_seconds` (#10972) records,
for each holder fan-out pass, the total time spent in each of four sites, one
histogram sample per site the pass entered. Labels are `domain` and
`fanout_site`; no scope, holder or CVE id is a label.

| `fanout_site` | What it times |
| --- | --- |
| `inflight_read` | The claimed-or-running queue read that finds the holders to flag. |
| `holder_reads` | Every holder lookup statement of the pass, summed. The per-statement view stays on `eshu_dp_supply_chain_impact_fanout_read_duration_seconds`. |
| `reopen_loop` | The per-holder reopen or replay-flag writes, summed. |
| `state_update` | The replay-flag clear and the calling item's fan-out state write. |

The same totals ride on the `supply_chain_impact.fanout` span as
`inflight_read_seconds`, `holder_reads_seconds`, `reopen_loop_seconds` and
`state_update_seconds`. A pass that fails inside a site still reports the time
it spent there, so a timed-out read shows up. A pass with nothing to fan out
enters no site and reports nothing. The sites do not cover the writer commit or
the advisory-group enqueue, which run before the fan-out.
