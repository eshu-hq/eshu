# Supply-Chain Active Evidence Signals

A supply-chain impact pass loads its evidence by resolving matching fact IDs
from a fixed set of index-anchored statements, then fetching the facts by
primary key in 500-ID pages (#8880). Each statement has a stable name, so
these signals say which one a slow or surprising load spent its time in.

The `branch` label is a closed set: one name per statement in the
supply-chain impact branch catalog (for example `vulnerability_cve_id`,
`package_id`, `file_repo_id`, `vulnerability_suppression`,
`foreign_key_guard`), plus `scope_resolution` (repository IDs to active
scopes), `payload_page` (one primary-key page), and `identity` (one
canonical image-identity page). Statements that a pass's filter does not need
are not issued and record nothing.

- `eshu_dp_supply_chain_impact_active_evidence_branch_seconds` — histogram
  of one statement's wall time, labeled `branch`. A branch whose latency
  climbs while its row count holds names the index that stopped serving it.
- `eshu_dp_supply_chain_impact_active_evidence_branch_rows_total` — counter
  of rows one statement returned, labeled `branch`.
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
`active_evidence_truncated_loads` sub-signals. Read them next to
`sub_duration_load_active_evidence_seconds`.

## Evidence headroom

A pass may charge 100,000 expansion envelopes to its evidence budget. Above
that it goes partial: it upserts but retracts nothing, so a scope that crosses
the limit on every pass keeps its stale findings (#7154). Two gauges show the
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
did not retract) and `sub_signal_evidence_budget_ratio`, so a log search can
count partial passes directly.

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
| `eshu_dp_supply_chain_impact_os_advisory_joins_total` | counter | `source` (`debian`, `ubuntu`, `alpine`, `other`), `outcome` (`joined`, `not_joined`) | One increment per OS advisory package entry per pass. `joined` means at least one installed row of that vendor and package met it. A vendor whose `not_joined` rate climbs while `joined` stays at zero, on a fleet that runs that vendor's images, means the advisory key and the installed key have drifted apart again. |

Registry advisories (npm, PyPI, Go, and so on) are not counted. An advisory
that meets no row is normal for a package the fleet does not install, so read
the ratio per vendor, not the raw `not_joined` count.

The success line carries the same totals for one pass as the
`os_advisories_considered` and `os_advisories_joined` sub-signals, written even
when zero. `os_advisories_considered` above zero with `os_advisories_joined` at
zero is the "loaded, none joined" shape.
