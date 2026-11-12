# #9805 chunked consume: keyed-apply plans at 400/1000/4000 identifiers

Refs #9805. Companion to [9805-chunked-consume-linearity.md](9805-chunked-consume-linearity.md), which holds the method, the result table, the deleted-node finding, the QA-anchor calibration and the markers. Each plan below is the measured (second, warm) `EXPLAIN (ANALYZE, BUFFERS)` of `applyWinnersKeyedSQL` over the stated identifier count, verbatim.

## Apply at 400 identifiers: 35.941 ms

```text
Insert on supply_chain_impact_winners_materialization  (cost=688.56..688.57 rows=1 width=10) (actual time=26.011..26.015 rows=1.00 loops=1)
  Conflict Resolution: UPDATE
  Conflict Arbiter Indexes: supply_chain_impact_winners_materialization_pkey
  Tuples Inserted: 0
  Conflicting Tuples: 1
  Buffers: shared hit=2344, local hit=132
  CTE winners_now
    ->  Nested Loop Left Join  (cost=177.58..177.80 rows=1 width=692) (actual time=7.219..23.168 rows=400.00 loops=1)
          Join Filter: ((concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))) = (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))))
          Buffers: shared hit=800, local hit=128
          CTE active_facts
            ->  Seq Scan on winners_keyed_active_facts  (cost=0.00..132.00 rows=400 width=1668) (actual time=0.003..0.042 rows=400.00 loops=1)
                  Buffers: local hit=128
          ->  Unique  (cost=33.51..33.56 rows=1 width=208) (actual time=7.128..9.525 rows=400.00 loops=1)
                Buffers: shared hit=800, local hit=128
                ->  Incremental Sort  (cost=33.51..33.56 rows=2 width=208) (actual time=7.128..9.454 rows=400.00 loops=1)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact.fact_id
                      Presorted Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                      Full-sort Groups: 13  Sort Method: quicksort  Average Memory: 73kB  Peak Memory: 73kB
                      Buffers: shared hit=800, local hit=128
                      ->  WindowAgg  (cost=33.38..33.50 rows=1 width=208) (actual time=6.915..9.242 rows=400.00 loops=1)
                            Window: w1 AS (PARTITION BY (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))))
                            Storage: Memory  Maximum Storage: 18kB
                            Buffers: shared hit=800, local hit=128
                            ->  Sort  (cost=33.38..33.39 rows=1 width=128) (actual time=6.903..6.922 rows=400.00 loops=1)
                                  Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                                  Sort Method: quicksort  Memory: 584kB
                                  Buffers: shared hit=800, local hit=128
                                  ->  Nested Loop Anti Join  (cost=21.85..33.37 rows=1 width=128) (actual time=0.041..6.518 rows=400.00 loops=1)
                                        Buffers: shared hit=800, local hit=128
                                        ->  Hash Left Join  (cost=21.70..32.77 rows=2 width=96) (actual time=0.010..0.316 rows=400.00 loops=1)
                                              Hash Cond: (fact.fact_id = finding_class.finding_fact_id)
                                              Filter: ((finding_class.finding_fact_id IS NULL) OR ((finding_class.source_class = 'static'::text) AND (finding_class.lifecycle = 'active'::text)))
                                              Buffers: local hit=128
                                              ->  CTE Scan on active_facts fact  (cost=0.00..10.00 rows=400 width=96) (actual time=0.005..0.245 rows=400.00 loops=1)
                                                    Filter: ((scope_id <> 'operator:vulnerability_suppressions'::text) AND (scope_id !~~ 'image://derived/%'::text))
                                                    Storage: Memory  Maximum Storage: 660kB
                                                    Buffers: local hit=128
                                              ->  Hash  (cost=15.20..15.20 rows=520 width=96) (actual time=0.002..0.002 rows=0.00 loops=1)
                                                    Buckets: 1024  Batches: 1  Memory Usage: 8kB
                                                    ->  Seq Scan on scanner_image_result_fact_classifications finding_class  (cost=0.00..15.20 rows=520 width=96) (actual time=0.002..0.002 rows=0.00 loops=1)
                                        ->  Index Only Scan using scanner_image_result_fact_associations_pkey on scanner_image_result_fact_associations finding_association  (cost=0.15..0.28 rows=1 width=32) (actual time=0.000..0.000 rows=0.00 loops=400)
                                              Index Cond: (finding_fact_id = fact.fact_id)
                                              Heap Fetches: 0
                                              Index Searches: 400
                                              Buffers: shared hit=800
          ->  Unique  (cost=12.07..12.07 rows=1 width=136) (actual time=0.000..0.000 rows=0.00 loops=400)
                ->  Sort  (cost=12.07..12.07 rows=1 width=136) (actual time=0.000..0.000 rows=0.00 loops=400)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact_1.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact_1.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact_1.fact_id
                      Sort Method: quicksort  Memory: 25kB
                      ->  CTE Scan on active_facts fact_1  (cost=0.00..12.06 rows=1 width=136) (actual time=0.043..0.043 rows=0.00 loops=1)
                            Filter: ((scope_id = 'operator:vulnerability_suppressions'::text) AND (COALESCE(NULLIF((payload ->> 'suppression_state'::text), ''::text), 'active'::text) <> 'active'::text))
                            Rows Removed by Filter: 400
                            Storage: Memory  Maximum Storage: 660kB
  CTE changed
    ->  Nested Loop Left Join  (cost=0.28..8.39 rows=1 width=693) (actual time=7.232..24.114 rows=400.00 loops=1)
          Filter: ((w.canonical_key IS NULL) OR (n.winner_fact_id IS DISTINCT FROM w.winner_fact_id) OR (n.winner_scope_id IS DISTINCT FROM w.winner_scope_id) OR (n.finding_id IS DISTINCT FROM w.finding_id) OR (n.priority_score IS DISTINCT FROM w.priority_score) OR (n.source_count IS DISTINCT FROM w.source_count) OR (n.impact_status IS DISTINCT FROM w.impact_status) OR (n.ecosystem IS DISTINCT FROM w.ecosystem) OR (n.severity_bucket IS DISTINCT FROM w.severity_bucket) OR (n.repository_id IS DISTINCT FROM w.repository_id) OR (n.cve_id IS DISTINCT FROM w.cve_id) OR (n.advisory_id IS DISTINCT FROM w.advisory_id) OR (n.package_id IS DISTINCT FROM w.package_id) OR (n.subject_digest IS DISTINCT FROM w.subject_digest) OR (n.image_ref IS DISTINCT FROM w.image_ref) OR (n.priority_bucket IS DISTINCT FROM w.priority_bucket) OR (n.detection_profile IS DISTINCT FROM w.detection_profile) OR (n.observed_version IS DISTINCT FROM w.observed_version) OR (n.match_reason IS DISTINCT FROM w.match_reason) OR (n.suppression_state IS DISTINCT FROM w.suppression_state) OR (n.suppression_expires_at IS DISTINCT FROM w.suppression_expires_at) OR (n.service_ids IS DISTINCT FROM w.service_ids) OR (n.workload_ids IS DISTINCT FROM w.workload_ids) OR (n.environments IS DISTINCT FROM w.environments))
          Buffers: shared hit=2000, local hit=128
          ->  CTE Scan on winners_now n  (cost=0.00..0.02 rows=1 width=692) (actual time=7.221..23.387 rows=400.00 loops=1)
                Storage: Memory  Maximum Storage: 204kB
                Buffers: shared hit=800, local hit=128
          ->  Index Scan using supply_chain_impact_canonical_winners_pkey on supply_chain_impact_canonical_winners w  (cost=0.28..8.30 rows=1 width=688) (actual time=0.001..0.001 rows=1.00 loops=400)
                Index Cond: (canonical_key = n.canonical_key)
                Index Searches: 400
                Buffers: shared hit=1200
  CTE upserted
    ->  Insert on supply_chain_impact_canonical_winners  (cost=0.00..0.02 rows=0 width=0) (actual time=9.563..9.563 rows=0.00 loops=1)
          Conflict Resolution: UPDATE
          Conflict Arbiter Indexes: supply_chain_impact_canonical_winners_pkey
          Tuples Inserted: 0
          Conflicting Tuples: 400
          Buffers: shared hit=13198 dirtied=32 written=32
          ->  CTE Scan on changed  (cost=0.00..0.02 rows=1 width=696) (actual time=0.001..0.104 rows=400.00 loops=1)
                Storage: Memory  Maximum Storage: 204kB
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=17.03..486.39 rows=169 width=68) (actual time=1.447..1.447 rows=0.00 loops=1)
          Buffers: shared hit=338, local hit=4
          ->  Hash Anti Join  (cost=17.03..486.39 rows=169 width=68) (actual time=1.446..1.447 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=338, local hit=4
                ->  Hash Semi Join  (cost=17.00..484.22 rows=169 width=44) (actual time=0.095..1.167 rows=400.00 loops=1)
                      Hash Cond: ((w_1.package_id = winners_keyed_idents.package_id) AND (w_1.repository_id = winners_keyed_idents.repository_id) AND (w_1.subject_digest = winners_keyed_idents.subject_digest) AND (COALESCE(NULLIF(w_1.cve_id, ''::text), NULLIF(w_1.advisory_id, ''::text), ''::text) = winners_keyed_idents.c0) AND (w_1.advisory_id = winners_keyed_idents.advisory_id))
                      Buffers: shared hit=338, local hit=4
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..391.99 rows=5399 width=198) (actual time=0.004..0.561 rows=5000.00 loops=1)
                            Buffers: shared hit=338
                      ->  Hash  (cost=8.00..8.00 rows=400 width=52) (actual time=0.088..0.088 rows=400.00 loops=1)
                            Buckets: 1024  Batches: 1  Memory Usage: 41kB
                            Buffers: local hit=4
                            ->  Seq Scan on winners_keyed_idents  (cost=0.00..8.00 rows=400 width=52) (actual time=0.003..0.027 rows=400.00 loops=1)
                                  Buffers: local hit=4
                ->  Hash  (cost=0.02..0.02 rows=1 width=88) (actual time=0.228..0.228 rows=400.00 loops=1)
                      Buckets: 1024  Batches: 1  Memory Usage: 236kB
                      ->  CTE Scan on winners_now n_1  (cost=0.00..0.02 rows=1 width=88) (actual time=0.003..0.125 rows=400.00 loops=1)
                            Storage: Memory  Maximum Storage: 204kB
  InitPlan 6
    ->  Aggregate  (cost=0.02..0.03 rows=1 width=8) (actual time=24.338..24.338 rows=1.00 loops=1)
          Buffers: shared hit=2000, local hit=128
          ->  CTE Scan on changed changed_1  (cost=0.00..0.02 rows=1 width=0) (actual time=24.337..24.337 rows=0.00 loops=1)
                Filter: is_new
                Rows Removed by Filter: 400
                Storage: Memory  Maximum Storage: 204kB
                Buffers: shared hit=2000, local hit=128
  InitPlan 7
    ->  Aggregate  (cost=0.02..0.03 rows=1 width=8) (actual time=0.067..0.067 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_2  (cost=0.00..0.02 rows=1 width=0) (actual time=0.000..0.051 rows=400.00 loops=1)
                Filter: (NOT is_new)
                Storage: Memory  Maximum Storage: 204kB
  InitPlan 8
    ->  Aggregate  (cost=3.80..3.81 rows=1 width=8) (actual time=1.447..1.447 rows=1.00 loops=1)
          Buffers: shared hit=338, local hit=4
          ->  CTE Scan on deleted  (cost=0.00..3.38 rows=169 width=0) (actual time=1.447..1.447 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
                Buffers: shared hit=338, local hit=4
  InitPlan 9
    ->  Aggregate  (cost=0.02..0.03 rows=1 width=8) (actual time=0.040..0.040 rows=1.00 loops=1)
          ->  CTE Scan on winners_now  (cost=0.00..0.02 rows=1 width=0) (actual time=0.000..0.024 rows=400.00 loops=1)
                Storage: Memory  Maximum Storage: 204kB
  InitPlan 10
    ->  Aggregate  (cost=0.02..0.03 rows=1 width=8) (actual time=0.040..0.040 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_3  (cost=0.00..0.02 rows=1 width=0) (actual time=0.000..0.024 rows=400.00 loops=1)
                Storage: Memory  Maximum Storage: 204kB
  InitPlan 11
    ->  Aggregate  (cost=0.02..0.03 rows=1 width=8) (actual time=0.039..0.039 rows=1.00 loops=1)
          ->  CTE Scan on winners_now winners_now_1  (cost=0.00..0.02 rows=1 width=0) (actual time=0.000..0.024 rows=400.00 loops=1)
                Storage: Memory  Maximum Storage: 204kB
  InitPlan 12
    ->  Aggregate  (cost=3.80..3.81 rows=1 width=8) (actual time=0.000..0.001 rows=1.00 loops=1)
          ->  CTE Scan on deleted deleted_1  (cost=0.00..3.38 rows=169 width=0) (actual time=0.000..0.000 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
  InitPlan 13
    ->  Index Scan using supply_chain_impact_winners_materialization_pkey on supply_chain_impact_winners_materialization supply_chain_impact_winners_materialization_1  (cost=0.15..8.17 rows=1 width=8) (actual time=0.008..0.008 rows=1.00 loops=1)
          Index Cond: (singleton = 1)
          Index Searches: 1
          Buffers: shared hit=2
  ->  Result  (cost=0.00..0.01 rows=1 width=10) (actual time=0.000..0.001 rows=1.00 loops=1)
Planning:
  Buffers: shared hit=59
Planning Time: 0.864 ms
Execution Time: 35.941 ms
```

## Apply at 1000 identifiers: 88.192 ms

```text
Insert on supply_chain_impact_winners_materialization  (cost=946.45..946.46 rows=1 width=10) (actual time=63.429..63.434 rows=1.00 loops=1)
  Conflict Resolution: UPDATE
  Conflict Arbiter Indexes: supply_chain_impact_winners_materialization_pkey
  Tuples Inserted: 0
  Conflicting Tuples: 1
  Buffers: shared hit=6202, local hit=272
  CTE winners_now
    ->  Merge Left Join  (cost=346.74..347.55 rows=3 width=692) (actual time=17.476..57.778 rows=1000.00 loops=1)
          Merge Cond: ((concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))) = (concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))))
          Buffers: shared hit=2000, local hit=256
          CTE active_facts
            ->  Seq Scan on winners_keyed_active_facts  (cost=0.00..266.00 rows=1000 width=1668) (actual time=0.003..0.095 rows=1000.00 loops=1)
                  Buffers: local hit=256
          ->  Unique  (cost=50.67..51.04 rows=3 width=208) (actual time=17.321..23.604 rows=1000.00 loops=1)
                Buffers: shared hit=2000, local hit=256
                ->  Incremental Sort  (cost=50.67..51.03 rows=3 width=208) (actual time=17.321..23.430 rows=1000.00 loops=1)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact.fact_id
                      Presorted Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                      Full-sort Groups: 32  Sort Method: quicksort  Average Memory: 73kB  Peak Memory: 73kB
                      Buffers: shared hit=2000, local hit=256
                      ->  WindowAgg  (cost=50.54..50.89 rows=3 width=208) (actual time=17.098..22.916 rows=1000.00 loops=1)
                            Window: w1 AS (PARTITION BY (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))))
                            Storage: Memory  Maximum Storage: 18kB
                            Buffers: shared hit=2000, local hit=256
                            ->  Sort  (cost=50.54..50.55 rows=3 width=128) (actual time=17.085..17.140 rows=1000.00 loops=1)
                                  Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                                  Sort Method: quicksort  Memory: 1423kB
                                  Buffers: shared hit=2000, local hit=256
                                  ->  Nested Loop Anti Join  (cost=21.85..50.52 rows=3 width=128) (actual time=0.042..16.105 rows=1000.00 loops=1)
                                        Buffers: shared hit=2000, local hit=256
                                        ->  Hash Left Join  (cost=21.70..49.34 rows=5 width=96) (actual time=0.012..0.781 rows=1000.00 loops=1)
                                              Hash Cond: (fact.fact_id = finding_class.finding_fact_id)
                                              Filter: ((finding_class.finding_fact_id IS NULL) OR ((finding_class.source_class = 'static'::text) AND (finding_class.lifecycle = 'active'::text)))
                                              Buffers: local hit=256
                                              ->  CTE Scan on active_facts fact  (cost=0.00..25.00 rows=1000 width=96) (actual time=0.004..0.604 rows=1000.00 loops=1)
                                                    Filter: ((scope_id <> 'operator:vulnerability_suppressions'::text) AND (scope_id !~~ 'image://derived/%'::text))
                                                    Storage: Memory  Maximum Storage: 1626kB
                                                    Buffers: local hit=256
                                              ->  Hash  (cost=15.20..15.20 rows=520 width=96) (actual time=0.001..0.001 rows=0.00 loops=1)
                                                    Buckets: 1024  Batches: 1  Memory Usage: 8kB
                                                    ->  Seq Scan on scanner_image_result_fact_classifications finding_class  (cost=0.00..15.20 rows=520 width=96) (actual time=0.001..0.001 rows=0.00 loops=1)
                                        ->  Index Only Scan using scanner_image_result_fact_associations_pkey on scanner_image_result_fact_associations finding_association  (cost=0.15..0.21 rows=1 width=32) (actual time=0.000..0.000 rows=0.00 loops=1000)
                                              Index Cond: (finding_fact_id = fact.fact_id)
                                              Heap Fetches: 0
                                              Index Searches: 1000
                                              Buffers: shared hit=2000
          ->  Unique  (cost=30.07..30.08 rows=1 width=136) (actual time=0.111..0.111 rows=0.00 loops=1)
                ->  Sort  (cost=30.07..30.07 rows=1 width=136) (actual time=0.111..0.111 rows=0.00 loops=1)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact_1.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact_1.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact_1.fact_id
                      Sort Method: quicksort  Memory: 25kB
                      ->  CTE Scan on active_facts fact_1  (cost=0.00..30.06 rows=1 width=136) (actual time=0.106..0.106 rows=0.00 loops=1)
                            Filter: ((scope_id = 'operator:vulnerability_suppressions'::text) AND (COALESCE(NULLIF((payload ->> 'suppression_state'::text), ''::text), 'active'::text) <> 'active'::text))
                            Rows Removed by Filter: 1000
                            Storage: Memory  Maximum Storage: 1626kB
  CTE changed
    ->  Nested Loop Left Join  (cost=0.28..25.16 rows=3 width=693) (actual time=17.491..60.273 rows=1000.00 loops=1)
          Filter: ((w.canonical_key IS NULL) OR (n.winner_fact_id IS DISTINCT FROM w.winner_fact_id) OR (n.winner_scope_id IS DISTINCT FROM w.winner_scope_id) OR (n.finding_id IS DISTINCT FROM w.finding_id) OR (n.priority_score IS DISTINCT FROM w.priority_score) OR (n.source_count IS DISTINCT FROM w.source_count) OR (n.impact_status IS DISTINCT FROM w.impact_status) OR (n.ecosystem IS DISTINCT FROM w.ecosystem) OR (n.severity_bucket IS DISTINCT FROM w.severity_bucket) OR (n.repository_id IS DISTINCT FROM w.repository_id) OR (n.cve_id IS DISTINCT FROM w.cve_id) OR (n.advisory_id IS DISTINCT FROM w.advisory_id) OR (n.package_id IS DISTINCT FROM w.package_id) OR (n.subject_digest IS DISTINCT FROM w.subject_digest) OR (n.image_ref IS DISTINCT FROM w.image_ref) OR (n.priority_bucket IS DISTINCT FROM w.priority_bucket) OR (n.detection_profile IS DISTINCT FROM w.detection_profile) OR (n.observed_version IS DISTINCT FROM w.observed_version) OR (n.match_reason IS DISTINCT FROM w.match_reason) OR (n.suppression_state IS DISTINCT FROM w.suppression_state) OR (n.suppression_expires_at IS DISTINCT FROM w.suppression_expires_at) OR (n.service_ids IS DISTINCT FROM w.service_ids) OR (n.workload_ids IS DISTINCT FROM w.workload_ids) OR (n.environments IS DISTINCT FROM w.environments))
          Buffers: shared hit=5808, local hit=256
          ->  CTE Scan on winners_now n  (cost=0.00..0.06 rows=3 width=692) (actual time=17.477..58.345 rows=1000.00 loops=1)
                Storage: Memory  Maximum Storage: 485kB
                Buffers: shared hit=2000, local hit=256
          ->  Index Scan using supply_chain_impact_canonical_winners_pkey on supply_chain_impact_canonical_winners w  (cost=0.28..8.30 rows=1 width=688) (actual time=0.001..0.001 rows=1.00 loops=1000)
                Index Cond: (canonical_key = n.canonical_key)
                Index Searches: 1000
                Buffers: shared hit=3400
  CTE upserted
    ->  Insert on supply_chain_impact_canonical_winners  (cost=0.00..0.07 rows=0 width=0) (actual time=24.224..24.224 rows=0.00 loops=1)
          Conflict Resolution: UPDATE
          Conflict Arbiter Indexes: supply_chain_impact_canonical_winners_pkey
          Tuples Inserted: 0
          Conflicting Tuples: 1000
          Buffers: shared hit=32721 dirtied=59 written=59
          ->  CTE Scan on changed  (cost=0.00..0.07 rows=3 width=696) (actual time=0.001..0.236 rows=1000.00 loops=1)
                Storage: Memory  Maximum Storage: 485kB
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=48.60..558.07 rows=156 width=68) (actual time=2.090..2.091 rows=0.00 loops=1)
          Buffers: shared hit=388, local hit=16
          ->  Hash Anti Join  (cost=48.60..558.07 rows=156 width=68) (actual time=2.090..2.090 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=388, local hit=16
                ->  Hash Semi Join  (cost=48.50..556.00 rows=156 width=44) (actual time=0.220..1.400 rows=1000.00 loops=1)
                      Hash Cond: ((w_1.package_id = winners_keyed_idents.package_id) AND (w_1.repository_id = winners_keyed_idents.repository_id) AND (w_1.subject_digest = winners_keyed_idents.subject_digest) AND (COALESCE(NULLIF(w_1.cve_id, ''::text), NULLIF(w_1.advisory_id, ''::text), ''::text) = winners_keyed_idents.c0) AND (w_1.advisory_id = winners_keyed_idents.advisory_id))
                      Buffers: shared hit=388, local hit=16
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..437.93 rows=4993 width=198) (actual time=0.014..0.588 rows=5000.00 loops=1)
                            Buffers: shared hit=388
                      ->  Hash  (cost=26.00..26.00 rows=1000 width=52) (actual time=0.204..0.204 rows=1000.00 loops=1)
                            Buckets: 1024  Batches: 1  Memory Usage: 91kB
                            Buffers: local hit=16
                            ->  Seq Scan on winners_keyed_idents  (cost=0.00..26.00 rows=1000 width=52) (actual time=0.003..0.066 rows=1000.00 loops=1)
                                  Buffers: local hit=16
                ->  Hash  (cost=0.06..0.06 rows=3 width=88) (actual time=0.557..0.557 rows=1000.00 loops=1)
                      Buckets: 1024  Batches: 1  Memory Usage: 577kB
                      ->  CTE Scan on winners_now n_1  (cost=0.00..0.06 rows=3 width=88) (actual time=0.009..0.316 rows=1000.00 loops=1)
                            Storage: Memory  Maximum Storage: 485kB
  InitPlan 6
    ->  Aggregate  (cost=0.07..0.07 rows=1 width=8) (actual time=60.840..60.840 rows=1.00 loops=1)
          Buffers: shared hit=5808, local hit=256
          ->  CTE Scan on changed changed_1  (cost=0.00..0.06 rows=2 width=0) (actual time=60.839..60.839 rows=0.00 loops=1)
                Filter: is_new
                Rows Removed by Filter: 1000
                Storage: Memory  Maximum Storage: 485kB
                Buffers: shared hit=5808, local hit=256
  InitPlan 7
    ->  Aggregate  (cost=0.07..0.07 rows=1 width=8) (actual time=0.165..0.165 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_2  (cost=0.00..0.06 rows=2 width=0) (actual time=0.000..0.127 rows=1000.00 loops=1)
                Filter: (NOT is_new)
                Storage: Memory  Maximum Storage: 485kB
  InitPlan 8
    ->  Aggregate  (cost=3.51..3.52 rows=1 width=8) (actual time=2.091..2.091 rows=1.00 loops=1)
          Buffers: shared hit=388, local hit=16
          ->  CTE Scan on deleted  (cost=0.00..3.12 rows=156 width=0) (actual time=2.091..2.091 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
                Buffers: shared hit=388, local hit=16
  InitPlan 9
    ->  Aggregate  (cost=0.07..0.08 rows=1 width=8) (actual time=0.096..0.097 rows=1.00 loops=1)
          ->  CTE Scan on winners_now  (cost=0.00..0.06 rows=3 width=0) (actual time=0.000..0.059 rows=1000.00 loops=1)
                Storage: Memory  Maximum Storage: 485kB
  InitPlan 10
    ->  Aggregate  (cost=0.07..0.08 rows=1 width=8) (actual time=0.103..0.103 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_3  (cost=0.00..0.06 rows=3 width=0) (actual time=0.000..0.065 rows=1000.00 loops=1)
                Storage: Memory  Maximum Storage: 485kB
  InitPlan 11
    ->  Aggregate  (cost=0.07..0.08 rows=1 width=8) (actual time=0.096..0.096 rows=1.00 loops=1)
          ->  CTE Scan on winners_now winners_now_1  (cost=0.00..0.06 rows=3 width=0) (actual time=0.000..0.059 rows=1000.00 loops=1)
                Storage: Memory  Maximum Storage: 485kB
  InitPlan 12
    ->  Aggregate  (cost=3.51..3.52 rows=1 width=8) (actual time=0.000..0.001 rows=1.00 loops=1)
          ->  CTE Scan on deleted deleted_1  (cost=0.00..3.12 rows=156 width=0) (actual time=0.000..0.000 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
  InitPlan 13
    ->  Index Scan using supply_chain_impact_winners_materialization_pkey on supply_chain_impact_winners_materialization supply_chain_impact_winners_materialization_1  (cost=0.15..8.17 rows=1 width=8) (actual time=0.007..0.008 rows=1.00 loops=1)
          Index Cond: (singleton = 1)
          Index Searches: 1
          Buffers: shared hit=2
  ->  Result  (cost=0.00..0.01 rows=1 width=10) (actual time=0.000..0.001 rows=1.00 loops=1)
Planning:
  Buffers: shared hit=59
Planning Time: 0.847 ms
Execution Time: 88.192 ms
```

## Apply at 4000 identifiers: 371.061 ms

```text
Insert on supply_chain_impact_winners_materialization  (cost=2366.55..2366.56 rows=1 width=10) (actual time=256.250..256.256 rows=1.00 loops=1)
  Conflict Resolution: UPDATE
  Conflict Arbiter Indexes: supply_chain_impact_winners_materialization_pkey
  Tuples Inserted: 0
  Conflicting Tuples: 1
  Buffers: shared hit=26585, local hit=246 read=842 written=141, temp read=1492 written=1493
  CTE winners_now
    ->  Merge Left Join  (cost=1321.00..1326.39 rows=18 width=692) (actual time=75.543..236.485 rows=4000.00 loops=1)
          Merge Cond: ((concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))) = (concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))))
          Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=1492 written=1493
          CTE active_facts
            ->  Seq Scan on winners_keyed_active_facts  (cost=0.00..1064.00 rows=4000 width=1668) (actual time=0.013..1.365 rows=4000.00 loops=1)
                  Buffers: local hit=244 read=780 written=141
          ->  Unique  (cost=136.93..139.78 rows=18 width=208) (actual time=74.429..101.025 rows=4000.00 loops=1)
                Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=696 written=1492
                ->  Incremental Sort  (cost=136.93..139.73 rows=18 width=208) (actual time=74.428..100.344 rows=4000.00 loops=1)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact.fact_id
                      Presorted Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                      Full-sort Groups: 125  Sort Method: quicksort  Average Memory: 73kB  Peak Memory: 73kB
                      Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=696 written=1492
                      ->  WindowAgg  (cost=136.81..138.92 rows=18 width=208) (actual time=74.202..98.363 rows=4000.00 loops=1)
                            Window: w1 AS (PARTITION BY (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text))))
                            Storage: Memory  Maximum Storage: 18kB
                            Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=696 written=1492
                            ->  Sort  (cost=136.81..136.85 rows=18 width=128) (actual time=74.187..75.274 rows=4000.00 loops=1)
                                  Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact.payload ->> 'cve_id'::text), ''::text), NULLIF((fact.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact.payload ->> 'package_id'::text), ''::text), COALESCE((fact.payload ->> 'purl'::text), ''::text), COALESCE((fact.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact.payload ->> 'observed_version'::text), ''::text), COALESCE((fact.payload ->> 'requested_range'::text), ''::text), COALESCE((fact.payload ->> 'impact_status'::text), ''::text), COALESCE((fact.payload ->> 'repository_id'::text), ''::text), COALESCE((fact.payload ->> 'subject_digest'::text), ''::text)))
                                  Sort Method: external merge  Disk: 5568kB
                                  Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=696 written=1492
                                  ->  Nested Loop Anti Join  (cost=21.85..136.43 rows=18 width=128) (actual time=0.056..67.000 rows=4000.00 loops=1)
                                        Buffers: shared hit=8000, local hit=244 read=780 written=141, temp written=795
                                        ->  Hash Left Join  (cost=21.70..132.22 rows=20 width=96) (actual time=0.021..5.613 rows=4000.00 loops=1)
                                              Hash Cond: (fact.fact_id = finding_class.finding_fact_id)
                                              Filter: ((finding_class.finding_fact_id IS NULL) OR ((finding_class.source_class = 'static'::text) AND (finding_class.lifecycle = 'active'::text)))
                                              Buffers: local hit=244 read=780 written=141, temp written=795
                                              ->  CTE Scan on active_facts fact  (cost=0.00..100.00 rows=4000 width=96) (actual time=0.015..4.938 rows=4000.00 loops=1)
                                                    Filter: ((scope_id <> 'operator:vulnerability_suppressions'::text) AND (scope_id !~~ 'image://derived/%'::text))
                                                    Storage: Disk  Maximum Storage: 6367kB
                                                    Buffers: local hit=244 read=780 written=141, temp written=795
                                              ->  Hash  (cost=15.20..15.20 rows=520 width=96) (actual time=0.002..0.002 rows=0.00 loops=1)
                                                    Buckets: 1024  Batches: 1  Memory Usage: 8kB
                                                    ->  Seq Scan on scanner_image_result_fact_classifications finding_class  (cost=0.00..15.20 rows=520 width=96) (actual time=0.001..0.001 rows=0.00 loops=1)
                                        ->  Index Only Scan using scanner_image_result_fact_associations_pkey on scanner_image_result_fact_associations finding_association  (cost=0.15..0.18 rows=1 width=32) (actual time=0.000..0.000 rows=0.00 loops=4000)
                                              Index Cond: (finding_fact_id = fact.fact_id)
                                              Heap Fetches: 0
                                              Index Searches: 4000
                                              Buffers: shared hit=8000
          ->  Unique  (cost=120.07..120.08 rows=1 width=136) (actual time=1.069..1.069 rows=0.00 loops=1)
                Buffers: temp read=796 written=1
                ->  Sort  (cost=120.07..120.07 rows=1 width=136) (actual time=1.068..1.069 rows=0.00 loops=1)
                      Sort Key: (concat_ws('|'::text, COALESCE(NULLIF((fact_1.payload ->> 'cve_id'::text), ''::text), NULLIF((fact_1.payload ->> 'advisory_id'::text), ''::text), ''::text), COALESCE((fact_1.payload ->> 'advisory_id'::text), ''::text), COALESCE((fact_1.payload ->> 'package_id'::text), ''::text), COALESCE((fact_1.payload ->> 'purl'::text), ''::text), COALESCE((fact_1.payload ->> 'product_criteria'::text), ''::text), COALESCE((fact_1.payload ->> 'match_criteria_id'::text), ''::text), COALESCE((fact_1.payload ->> 'observed_version'::text), ''::text), COALESCE((fact_1.payload ->> 'requested_range'::text), ''::text), COALESCE((fact_1.payload ->> 'impact_status'::text), ''::text), COALESCE((fact_1.payload ->> 'repository_id'::text), ''::text), COALESCE((fact_1.payload ->> 'subject_digest'::text), ''::text))), (COALESCE((NULLIF((fact_1.payload ->> 'priority_score'::text), ''::text))::integer, 0)) DESC, (CASE WHEN (NULLIF((fact_1.payload ->> 'finding_id'::text), ''::text) IS NULL) THEN 0 ELSE 1 END) DESC, fact_1.fact_id
                      Sort Method: quicksort  Memory: 25kB
                      Buffers: temp read=796 written=1
                      ->  CTE Scan on active_facts fact_1  (cost=0.00..120.06 rows=1 width=136) (actual time=1.062..1.062 rows=0.00 loops=1)
                            Filter: ((scope_id = 'operator:vulnerability_suppressions'::text) AND (COALESCE(NULLIF((payload ->> 'suppression_state'::text), ''::text), 'active'::text) <> 'active'::text))
                            Rows Removed by Filter: 4000
                            Storage: Disk  Maximum Storage: 6367kB
                            Buffers: temp read=796 written=1
  CTE changed
    ->  Nested Loop Left Join  (cost=0.41..149.22 rows=18 width=693) (actual time=75.564..246.758 rows=4000.00 loops=1)
          Filter: ((w.canonical_key IS NULL) OR (n.winner_fact_id IS DISTINCT FROM w.winner_fact_id) OR (n.winner_scope_id IS DISTINCT FROM w.winner_scope_id) OR (n.finding_id IS DISTINCT FROM w.finding_id) OR (n.priority_score IS DISTINCT FROM w.priority_score) OR (n.source_count IS DISTINCT FROM w.source_count) OR (n.impact_status IS DISTINCT FROM w.impact_status) OR (n.ecosystem IS DISTINCT FROM w.ecosystem) OR (n.severity_bucket IS DISTINCT FROM w.severity_bucket) OR (n.repository_id IS DISTINCT FROM w.repository_id) OR (n.cve_id IS DISTINCT FROM w.cve_id) OR (n.advisory_id IS DISTINCT FROM w.advisory_id) OR (n.package_id IS DISTINCT FROM w.package_id) OR (n.subject_digest IS DISTINCT FROM w.subject_digest) OR (n.image_ref IS DISTINCT FROM w.image_ref) OR (n.priority_bucket IS DISTINCT FROM w.priority_bucket) OR (n.detection_profile IS DISTINCT FROM w.detection_profile) OR (n.observed_version IS DISTINCT FROM w.observed_version) OR (n.match_reason IS DISTINCT FROM w.match_reason) OR (n.suppression_state IS DISTINCT FROM w.suppression_state) OR (n.suppression_expires_at IS DISTINCT FROM w.suppression_expires_at) OR (n.service_ids IS DISTINCT FROM w.service_ids) OR (n.workload_ids IS DISTINCT FROM w.workload_ids) OR (n.environments IS DISTINCT FROM w.environments))
          Buffers: shared hit=26016, local hit=244 read=780 written=141, temp read=1492 written=1493
          ->  CTE Scan on winners_now n  (cost=0.00..0.36 rows=18 width=692) (actual time=75.544..238.687 rows=4000.00 loops=1)
                Storage: Memory  Maximum Storage: 1908kB
                Buffers: shared hit=8000, local hit=244 read=780 written=141, temp read=1492 written=1493
          ->  Index Scan using supply_chain_impact_canonical_winners_pkey on supply_chain_impact_canonical_winners w  (cost=0.41..8.20 rows=1 width=688) (actual time=0.001..0.001 rows=1.00 loops=4000)
                Index Cond: (canonical_key = n.canonical_key)
                Index Searches: 4000
                Buffers: shared hit=16992
  CTE upserted
    ->  Insert on supply_chain_impact_canonical_winners  (cost=0.00..0.40 rows=0 width=0) (actual time=112.722..112.722 rows=0.00 loops=1)
          Conflict Resolution: UPDATE
          Conflict Arbiter Indexes: supply_chain_impact_canonical_winners_pkey
          Tuples Inserted: 0
          Conflicting Tuples: 4000
          Buffers: shared hit=139049 dirtied=335 written=412
          ->  CTE Scan on changed  (cost=0.00..0.40 rows=18 width=696) (actual time=0.001..0.941 rows=4000.00 loops=1)
                Storage: Memory  Maximum Storage: 1908kB
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=194.59..873.64 rows=148 width=68) (actual time=5.354..5.355 rows=0.00 loops=1)
          Buffers: shared hit=563, local hit=2 read=62
          ->  Hash Anti Join  (cost=194.59..873.64 rows=148 width=68) (actual time=5.353..5.353 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=563, local hit=2 read=62
                ->  Hash Semi Join  (cost=194.00..871.19 rows=149 width=44) (actual time=0.921..2.694 rows=4000.00 loops=1)
                      Hash Cond: ((w_1.package_id = winners_keyed_idents.package_id) AND (w_1.repository_id = winners_keyed_idents.repository_id) AND (w_1.subject_digest = winners_keyed_idents.subject_digest) AND (COALESCE(NULLIF(w_1.cve_id, ''::text), NULLIF(w_1.advisory_id, ''::text), ''::text) = winners_keyed_idents.c0) AND (w_1.advisory_id = winners_keyed_idents.advisory_id))
                      Buffers: shared hit=563, local hit=2 read=62
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..610.71 rows=4771 width=198) (actual time=0.036..0.717 rows=5000.00 loops=1)
                            Buffers: shared hit=563
                      ->  Hash  (cost=104.00..104.00 rows=4000 width=52) (actual time=0.876..0.877 rows=4000.00 loops=1)
                            Buckets: 4096  Batches: 1  Memory Usage: 361kB
                            Buffers: local hit=2 read=62
                            ->  Seq Scan on winners_keyed_idents  (cost=0.00..104.00 rows=4000 width=52) (actual time=0.013..0.333 rows=4000.00 loops=1)
                                  Buffers: local hit=2 read=62
                ->  Hash  (cost=0.36..0.36 rows=18 width=88) (actual time=2.130..2.130 rows=4000.00 loops=1)
                      Buckets: 4096 (originally 1024)  Batches: 1 (originally 1)  Memory Usage: 2306kB
                      ->  CTE Scan on winners_now n_1  (cost=0.00..0.36 rows=18 width=88) (actual time=0.005..1.257 rows=4000.00 loops=1)
                            Storage: Memory  Maximum Storage: 1908kB
  InitPlan 6
    ->  Aggregate  (cost=0.38..0.39 rows=1 width=8) (actual time=249.007..249.007 rows=1.00 loops=1)
          Buffers: shared hit=26016, local hit=244 read=780 written=141, temp read=1492 written=1493
          ->  CTE Scan on changed changed_1  (cost=0.00..0.36 rows=9 width=0) (actual time=249.006..249.006 rows=0.00 loops=1)
                Filter: is_new
                Rows Removed by Filter: 4000
                Storage: Memory  Maximum Storage: 1908kB
                Buffers: shared hit=26016, local hit=244 read=780 written=141, temp read=1492 written=1493
  InitPlan 7
    ->  Aggregate  (cost=0.38..0.39 rows=1 width=8) (actual time=0.654..0.654 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_2  (cost=0.00..0.36 rows=9 width=0) (actual time=0.001..0.506 rows=4000.00 loops=1)
                Filter: (NOT is_new)
                Storage: Memory  Maximum Storage: 1908kB
  InitPlan 8
    ->  Aggregate  (cost=3.33..3.34 rows=1 width=8) (actual time=5.355..5.355 rows=1.00 loops=1)
          Buffers: shared hit=563, local hit=2 read=62
          ->  CTE Scan on deleted  (cost=0.00..2.96 rows=148 width=0) (actual time=5.354..5.354 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
                Buffers: shared hit=563, local hit=2 read=62
  InitPlan 9
    ->  Aggregate  (cost=0.40..0.41 rows=1 width=8) (actual time=0.403..0.403 rows=1.00 loops=1)
          ->  CTE Scan on winners_now  (cost=0.00..0.36 rows=18 width=0) (actual time=0.000..0.238 rows=4000.00 loops=1)
                Storage: Memory  Maximum Storage: 1908kB
  InitPlan 10
    ->  Aggregate  (cost=0.40..0.41 rows=1 width=8) (actual time=0.384..0.384 rows=1.00 loops=1)
          ->  CTE Scan on changed changed_3  (cost=0.00..0.36 rows=18 width=0) (actual time=0.000..0.239 rows=4000.00 loops=1)
                Storage: Memory  Maximum Storage: 1908kB
  InitPlan 11
    ->  Aggregate  (cost=0.40..0.41 rows=1 width=8) (actual time=0.401..0.402 rows=1.00 loops=1)
          ->  CTE Scan on winners_now winners_now_1  (cost=0.00..0.36 rows=18 width=0) (actual time=0.000..0.241 rows=4000.00 loops=1)
                Storage: Memory  Maximum Storage: 1908kB
  InitPlan 12
    ->  Aggregate  (cost=3.33..3.34 rows=1 width=8) (actual time=0.001..0.001 rows=1.00 loops=1)
          ->  CTE Scan on deleted deleted_1  (cost=0.00..2.96 rows=148 width=0) (actual time=0.000..0.000 rows=0.00 loops=1)
                Storage: Memory  Maximum Storage: 17kB
  InitPlan 13
    ->  Index Scan using supply_chain_impact_winners_materialization_pkey on supply_chain_impact_winners_materialization supply_chain_impact_winners_materialization_1  (cost=0.15..8.17 rows=1 width=8) (actual time=0.014..0.015 rows=1.00 loops=1)
          Index Cond: (singleton = 1)
          Index Searches: 1
          Buffers: shared hit=2
  ->  Result  (cost=0.00..0.01 rows=1 width=10) (actual time=0.001..0.001 rows=1.00 loops=1)
Planning:
  Buffers: shared hit=59
Planning Time: 0.876 ms
Execution Time: 371.061 ms
```
