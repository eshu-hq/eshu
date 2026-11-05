<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Removal-tail PREP: removal-PR docs list (#9669)

Refs #9669, part of #6707. Status: preparation only. The removal PR must
update every doc below in the same PR (issue acceptance: "Full gates
green on the removal PR; docs updated in the same PR"). Exact paths, with
the line or section that must change.

| # | Path | What must change in the removal PR |
| --- | --- | --- |
| 1 | `go/cmd/api/CUTOVER_OWNERSHIP.md` | Status line ("Status: preparation only — no cutover has occurred, and no in-tree implementation moves in this artifact") flips to retired/superseded with a pointer to the serving satellite revision; the route table is deleted with the code or archived per the Leaf B decision |
| 2 | `docs/public/architecture.md` | Ownership section (lines 58–69: "API and MCP own bounded reads from canonical graph and relational state"; the all-scopes suppression-mutation note) reflects satellite ownership of the HTTP read surface and the admission-contract boundary |
| 3 | `docs/public/deployment/service-runtimes.md` | Runtime Map API row (line 15) and the "Helm starts API and MCP through the `eshu` CLI wrapper" note (line 27) point at the satellite deployment; the shared admin-surface section stays only for runtimes that keep it |
| 4 | `docs/public/deployment/service-runtimes-core.md` | API row (line 28: `eshu api start …`, "HTTP query/admin reads plus the all-scopes vulnerability-suppression policy mutation") and scaling notes (line 41) move to or reference the eshu-api repo docs |
| 5 | `docs/public/reference/http-api.md` | Lockstep source for the HTTP contract (`go/internal/query/openapi/`, handler tests): either retires with the in-tree surface or becomes a pointer to the satellite-owned reference |
| 6 | `go/cmd/api/README.md` | Lifecycle, env vars, and operational notes for the in-tree binary: retire or redirect to the eshu-api repo |
| 7 | `go/cmd/README.md` | Binary table row (line 20: `` `eshu-api` \| `api/` \| Long-running HTTP API ``) and the `api --> postgres` / `api --> graph` edges (lines 65–66): mark the satellite boundary |
| 8 | Epic #6707 row | Removal PR body opens with `Refs #9669, part of #6707` (repo PR-text rule: body opens with the `Refs` line and a bold lead) and carries the "At a glance" table plus the retired-coverage honesty note from the sibling skeleton |

Out of scope for this lane: the edits themselves. This list only names
them so the removal PR (post Leaves A–G plus fresh arbiter review) makes
them in the same diff.
