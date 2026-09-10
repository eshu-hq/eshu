# Story Leaf — Evidence (#6060 naming follow-up)

Moves the relationship-story shapers out of `codequery/` into the new
`relationships/story/` leaf per the #6618 naming rules (rule 3: split glued
compounds into nested directories): `class.go`, `data.go`, `graph.go`,
`nornicdb.go`, `resolution.go`, with the thin `*CodeHandler` methods left in
`codequery/story_handlers.go`, `story_reads.go`, and `story_nornicdb.go`.
One family test (`relationship_story_evidence_state_test.go`) moved with
them; the story suites that construct the root `CodeHandler`, drive the
pinned NornicDB cypher builders, or share the root grant fake stay in
`codequery/` — a different package can name neither — as do the suites
owned by #5167.

The pinned builders stay where the pins point: `hot-cypher.yaml` still
files `nornicDBRelationshipStoryGraphCypher`,
`nornicDBRelationshipStoryInheritanceDepthCypher`, and
`(*CodeHandler).resolveNornicDBRelationshipStoryAnchorProperty` at
`story_nornicdb.go` with unchanged `source_sha256`, and the exported
wrappers stay because the root-package queryplan binding test must bind
entity, impact, and infra handlers alongside the code builders. Moving them
would be an API change, not a move.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction. No Cypher text
changed; the hot-cypher entries repath nothing and their `cypher_sha256`
values are untouched. Package tests green: query, codequery,
relationships/story, queryplan.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed.
