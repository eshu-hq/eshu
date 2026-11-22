# Arbiter ruling: #11208 partial fact_records index or accepted cost (2026-11-22, arbiter-eshu, read-only)

RULING: (b). No new `fact_records` index. Close #11208 as an accepted, measured cost, with the wording below. The budget row stays named; only its comment changes.

Verified: the lane's figures are linear and agree with the gate (2,431 / 800 = 3.04 and 9,715 / 3,200 = 3.04 buffers per scope; the #11170 gate read 2,455 at 800 scopes). The named row is `testdata/benchmarks/read-api-route-budgets.txt:131` at 1500 ms. No existing index leads with `fact_kind` for the two SSM kinds (`git grep` on `origin/main`). The QA environment runs an SSM collector, so QA is the non-empty case. #11170 is closed. I did not rerun the lane's scripts.

1. Why not (a):
   - The index only helps a deployment with no SSM facts. With facts present it changes nothing (62,069 to 61,255 buffers, about 200 ms).
   - The saved cost is small: 16 ms at 3,200 scopes, warm. At that slope a 9,000-scope install pays about 27,000 buffers.
   - It works against the epic. #10880 PR-D reports the index count falling from 152 to 142, and #10881 multiplies every `fact_records` index by the part count.
   - It would need a build over a 415 GB table and a write-cost proof on the real insert path. Neither is measured.
2. The `collector_kind='aws'` rewrite stays rejected: nothing enforces that SSM facts live only in AWS scopes, so it can drop rows.
3. Close #11208 as "not planned" with this comment, after decision 4:
   "Cause confirmed: the read probes `fact_records_scope_generation_idx` once per active scope, about 3.0 buffers per scope, also when no SSM facts exist (2,431 buffers and 4.2 to 4.6 ms at 800 scopes; 9,715 buffers and 16 ms at 3,200; PostgreSQL 18.6, warm, synthetic fixture). A partial index would make the empty answer constant (4 buffers) but does not help when SSM facts exist, and it adds an index to the table the epic is removing indexes from. Arbiter ruling arbiter-11208-index accepts the cost and rejects the index. Criterion 2 is closed by that ruling, not by a fix. Criterion 3: the named row stays at 1500 ms with the measured slope in its comment. Reopen if an empty answer on this route passes 150 ms p95, or a deployment passes 30,000 active scopes."
4. Before posting: diff `q_base.sql` against the SQL that `buildSSMHostActiveFactsSQL` emits for `AllScopes` at the default limit, and say in the comment that they match. The note is hand-copied today. If they differ, re-measure first.
5. Doc change: yes, one line. Edit the comment on `read-api-route-budgets.txt:131` to cite the measured slope and this ruling. Leave the 1500 ms ceiling and the two work-budget rows as they are. It rides any open PR that touches the gate tables, or goes as a tier-S commit.
6. The non-empty sort (all active SSM facts sorted before the host-rank cap, temp spill at 84,280 rows) is a real observation and not #11208. #11170 is closed, so put it in the #11208 closing comment and as one line on #10860. No new issue. It is a theory until someone reads a plan on real SSM data.

Will not: approve a `fact_records` index on a synthetic, warm-only proof; accept the AWS prefilter; lower the gate row to a value no fix produced.

NOT_CHECKED: the lane's scripts and fixture (not rerun); cold-cache cost; the real QA plan and SSM row count; whether `q_base.sql` equals the builder's SQL; the 150 ms and 30,000-scope reopen lines are my thresholds, derived from the measured slope, not from a contract.
