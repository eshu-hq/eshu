# Supply-Chain Impact Handler Attribution Evidence (#4429)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Supply-Chain Impact Handler Attribution (#4429)

`supply_chain_impact` now emits reducer result sub-durations and bounded
sub-signals for the handler path that dominated a full-corpus run in #3624. The
change is diagnostic only: it does not change fact selection predicates, active
evidence expansion, matching logic, suppression evaluation, reducer fact writes,
Cypher, graph writes, worker counts, batch sizes, or queue conflict keys.

No-Regression Evidence: `go test ./internal/reducer -run
TestSupplyChainImpactHandlerSubDurationsAndSignals -count=1` failed first
because `SupplyChainImpactHandler.Handle` returned nil `SubDurations`, then
passed after the handler populated timing and signal maps. The added work is
bounded to `time.Now()` diffs around existing phases plus one small result map
for durations and one small signal map for counts/flags; no Postgres query shape
or graph/backend write path changed.

Observability Evidence: the existing reducer service log path emits the new
durations as `sub_duration_{load_scope_facts,load_repository_facts,
load_manifest_dependencies,load_active_evidence,load_python_reachability,
load_jvm_reachability,security_alert_scoping,build_findings,
evaluate_suppressions,write_findings,emit_counters,total}_seconds` and the
bounded counts/flags as
`sub_signal_{input_ready,written_rows,scope_facts,repository_facts,
manifest_dependency_facts,active_evidence_facts,python_reachability_facts,
jvm_reachability_facts,post_scope_facts,security_alert_scoping_applied,
security_alert_scoped_out_facts,findings,active_evidence_truncated}`. These
fields let an operator separate source fact load, broad active-evidence
expansion, reachability enrichment, provider security-alert narrowing,
in-memory finding/suppression work, durable reducer fact writes, and counter
emission from the single `handler_duration_seconds` value without adding new
metrics or labels.
