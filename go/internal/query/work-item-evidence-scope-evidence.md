# Work-Item Evidence Scope Evidence

`GET /api/v0/work-items/evidence` (and the MCP `list_work_item_evidence` tool
that shares the handler) is a source-only Jira/work-item evidence read. Its facts
key on the provider project scope (`scope_id`, `project_key`, `work_item_key`),
not a git repository, so scoped-token authorization cannot use the provider
scope directly.

The durable join is `linked_repository_id`: the Jira collector resolves a
confidently typed GitHub pull-request or GitLab merge-request link URL to a
canonical repository id via `repositoryidentity.CanonicalRepositoryID` **before**
the raw URL is redacted, and persists only that id on the
`work_item.external_link` fact (issue #2160 / PR #2182). It is the same
generation-independent identifier Eshu stores for every repository and carries no
raw URL, query parameter, credential, ACL principal, or user identity.

Scoped enforcement (issue #2142):

- **Gate.** `scopedWorkItemEvidenceRoute` in `auth_scoped_routes.go` adds the
  exact evidence path to `scopedHTTPRouteSupportsTenantFilter`. Adjacent
  work-item sub-resources stay deny-by-default until each is separately proven
  tenant-filtered; the admin `POST /api/v0/admin/work-items/query` route stays
  admin-only.
- **Empty grant.** A scoped token with no grants returns the bounded
  zero-evidence page (`writeEmptyWorkItemEvidencePage`) without a store read.
- **Predicate.** `listWorkItemEvidenceQuery` intersects each fact's
  `linked_repository_id` with the grant array (`$9`) before `ORDER BY`/`LIMIT`:
  `cardinality($9) = 0 OR fact.payload->>'linked_repository_id' = ANY($9)`. An
  empty array (shared/admin/local) keeps the unscoped all-rows branch; a
  non-empty array bounds the page.
- **Fail-closed.** A work item with no `linked_repository_id` — every fact kind
  except a canonicalized external link, or an unresolved/out-of-grant project
  selector — fails the `ANY()` match and stays invisible to scoped tokens. The
  worst case is under-exposure, never a provider-scope leak.
- **Multi-repo.** A work item linked to multiple repositories is visible for the
  granted subset only, because `= ANY($9)` matches whenever any granted id is in
  the grant array.
- **No raw provider identifiers** appear in metric labels; the route reuses the
  existing `query.work_item_evidence` span and result counters.

No-Regression Evidence:
`go test ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1`
covers the suites. The focused proof is
`go test ./internal/query ./internal/mcp -run 'WorkItemEvidence|ScopedTokensAllowsWorkItem|ScopedTokensRejectsAdjacentWorkItem|DispatchToolWorkItemEvidence' -count=1`,
which fails if the gate stops allowing the route, the empty grant reads the
store, the handler stops forwarding the grant set, the SQL predicate drops the
`linked_repository_id` intersection or its placement before `ORDER BY`/`LIMIT`,
or API/MCP parity regresses. `go test ./internal/query -race -run 'WorkItemEvidence'`
proves the scoped path is race-clean.

No-Observability-Change: the change adds no new read model, graph query, reducer
lane, worker, queue, metric, span, or log contract. The grant predicate is a
bound array on the existing bounded work-item evidence query; the empty-grant
page reuses the existing truth envelope and response shape.
