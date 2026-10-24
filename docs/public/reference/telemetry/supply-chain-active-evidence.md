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
