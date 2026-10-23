# #8797: service tools accept service_id and name the next selector

`get_service_story` told a caller to add `service_id`, `repo`, or `environment`
when a service name was ambiguous, but the tool rejected `service_id`, and
`repo` cannot separate two workloads in one repository. This change accepts
`service_id` as a selector, adds a `next_selector` to each ambiguity candidate,
and corrects the error and tool text. It does not change identity resolution.

No-Regression Evidence: the lookup path is untouched. In
`go/internal/query/entity/service_workload_resolution.go` the diff changes one
error string. The candidate type and the function that renders candidates for
the 409 body (`serviceWorkloadCandidateMap`) move to
`service_workload_candidate_maps.go`; that function runs after the lookup
returned. No Cypher text, parameter, `LIMIT`, session, lock, queue, or batch
changes, so the graph round trips per request are the same as before. The extra work is one
two-key map per candidate, over at most `ServiceWorkloadCandidateLimit` (10)
candidates, on the ambiguous path only. The MCP route changes (get_service_story, get_service_intelligence_report, and
investigate_service share one precedence helper) are pure argument selection
with no I/O. The ambiguity proof is
`TestSameRepositoryCandidatesStayAmbiguousAndNameTheNextSelector`, which keeps
two same-repository workloads ambiguous and asserts the `next_selector` on both.

No-Observability-Change: the ambiguous outcome was already visible and still
is: the story route's 409 status in the HTTP request metrics, and the
`service_candidate_lookup` stage log (`row_count`, `truncated`) emitted by
`StartServiceQueryStage`. The change adds no stage, query, or failure path, so
a new counter would only repeat the 409 count by route.
