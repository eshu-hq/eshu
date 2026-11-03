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
are not issued and record nothing.

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
`sub_duration_active_evidence_branch_<branch>_seconds`, and the
`active_evidence_loads`, `active_evidence_resolved_fact_ids`,
`active_evidence_payload_pages`, `active_evidence_guard_rows`, and
`active_evidence_truncated_loads` sub-signals, plus the pass totals
`active_evidence_files_folded`, `active_evidence_payload_bytes_folded`, and
`active_evidence_payload_bytes_retained` (#9781). Read them next to
`sub_duration_load_active_evidence_seconds`.

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

An OS distribution advisory (Debian, Ubuntu, Alpine) becomes a finding only
when its package key meets an installed `os_package` row from an image scan.
A loaded advisory that meets no row produces nothing, so a broken join looks
the same as a fleet with nothing affected. Before #9848 that is what happened:
every OSV advisory with a versionless, qualified purl (all Debian and Alpine
records, and many Ubuntu ones) missed, because that purl never equalled the
installed row's key. One counter separates loaded from joined:

| Metric | Type | Key labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_os_advisory_joins_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `other`), `outcome` (`joined`, `not_joined`) | One increment per OS advisory package entry per index the pass evaluates: the fleet-wide index once, and on a protection-engaged pass each authorized scan target's index once, because the scanner worker's derived `os_package` rows exist only there. An advisory present in several indexes is counted in each. `joined` means at least one installed row of that vendor and package met it in that index; the counter still increments once per advisory entry per index. One joined entry on the fleet-wide pass can now produce one finding per image subject digest (#9858), so finding counts can exceed joined counts when images disagree. A vendor whose `not_joined` rate climbs while `joined` stays at zero, on a fleet that runs that vendor's images, means the advisory key and the installed key have drifted apart again. |

Registry advisories (npm, PyPI, Go, and so on) are not counted. An advisory
that meets no row is normal for a package the fleet does not install, so read
the ratio per vendor, not the raw `not_joined` count.

The success line carries the same totals for one pass as the
`os_advisories_considered` and `os_advisories_joined` sub-signals, written even
when zero. `os_advisories_considered` above zero with `os_advisories_joined` at
zero is the "loaded, none joined" shape.
