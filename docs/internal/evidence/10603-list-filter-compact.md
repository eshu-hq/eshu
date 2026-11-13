# List filter + compact projection evidence (#10603)
No-Regression Evidence: content fixture (FakePortContentStore, 500 repos, limit=500):
full baseline core body 300,626 B vs compact 58,129 B (total=500 count=500 both);
name-filtered compact page 455 B (total=1 count=1, filter before count+page).
Re-measured dispatch: dual 125,044 B, structured 58,473 B vs 262,144 budget;
graph COUNT/page share the same WHERE predicate, guards 400 pre-read.
No-Observability-Change: no new instruments; per-tool eshu_dp_mcp_response_bytes
(labeled tool+shape) plus eshu_dp_mcp_response_over_budget_total still emit; safe:
projection only drops keys post-annotation, predicate composes with access filter.
