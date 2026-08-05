# github_actions_workflows

Golden-corpus fixture for the GitHub Actions workflow-relationship detector
(#5337, #5378). `.github/workflows/ci.yml` carries three discriminating shapes
in one file:

- a genuine step-level `uses: hashicorp/setup-terraform@v3` that MUST produce a
  `DEPENDS_ON` / `github_actions_action_repository` content relationship whose
  `target_name` is the action repository slug `hashicorp/setup-terraform`, and
- a `run: |` block scalar whose heredoc text contains literal `uses:` lines
  (`octocat/example-action@v1`) that MUST NOT produce any relationship — the
  structured-YAML decode treats the block as an opaque string, and
- a reusable-workflow `with.image` input that emits input-only
  `ci.workflow_image_evidence` for the #5830 correlation floor. A separate,
  commit-matched CI run consumes the already observed container-ci-lineage
  image; the reducer may resolve its identity but must cap the outcome at
  `derived` because this workflow did not produce the image.

`actions/checkout@v4` is present but is excluded from `DEPENDS_ON` action edges
by design.

## Golden gate coverage, live-surface reachability & Ifá determination

The positive/foil discrimination is proven at the query-builder tier by
`TestGitHubActionsGoldenFixtureDiscriminatesRunBlockUses`, which feeds this
fixture's real `ci.yml` through `buildContentRelationshipSet` (the same
query-time content-relationship builder `get_entity_context` uses) and asserts
exactly one `github_actions_action_repository` edge — the genuine
`hashicorp/setup-terraform` step — while the `run:`-block `octocat/example-action`
literal produces none.

This relationship is **query-time only**, which shapes how it is (and is not)
covered by the live golden gate:

- It materializes **no** persisted graph edge and **no**
  `hashicorp/setup-terraform` graph node. Verified live against the golden corpus:
  the `ci.yml` `File` node has zero outgoing edges and no `setup-terraform` node
  exists. The `github_actions_action_repository` evidence kind only feeds the
  existing `repo_dependency` reducer materialized-edge family when the action
  target is an **in-corpus** repository; this fixture points at an external
  action, so nothing materializes.
- `ci.yml` is reachable as a content-only `File` entity. It has a canonical
  content-entity id derived from the fixture repository, workflow path, `File`
  label, `ci` name, and line 1; it still has no parser or graph counterpart.
  B-12 exercises that entity through both the HTTP entity-context route and the
  MCP `get_entity_context` tool. Each live shape requires
  `result_limits.relationship_count=1` and the exact `DEPENDS_ON`
  `hashicorp/setup-terraform` relationship. The paired mutation proof rejects a
  second `octocat/example-action` relationship, keeping the `run:`-block foil
  excluded from both surfaces.

Ifá materialized-edge coverage is **N/A**: no reducer/graph edge is produced for
this fixture's external action target, and the detector adds no
`reducer.MaterializedEdgeFamilies()` domain.

## Third, unrelated purpose: #5469 config_only version-resolution pin

`package.json` and `package-lock.json` in this directory are NOT part of the
GitHub Actions workflow-relationship or workflow-image fixtures above. They
give this repository a third, independent role: it has no cloud-runtime
evidence and no production claim from its CI/CD correlation. The #5830 run is
input-only and remains `derived`, so #5469's tiered version-resolution feature
still proves its `config_only` floor against a genuinely non-vacuous finding.

`package.json` declares an exact-pinned npm dependency on
`supply-chain-demo-lib@1.2.2`, matching `CVE-2026-00000`'s `affected_versions`
(`[1.2.2]`). `package-lock.json` resolves that dependency to a concrete
`observed_version`, so the finding has a judged version with no deployment
evidence above `config_only` -- exactly the case the golden pin at
`testdata/golden/e2e-20repo-snapshot.json`
(`GET /api/v0/supply-chain/impact/findings?limit=50&cve_id=CVE-2026-00000&profile=comprehensive`,
asserting `findings[].version_resolution_tier: config_only`) depends on.
Removing either file, or removing the `supply-chain-demo-lib` dependency from
them, empties `version_resolution_tier` for that finding and fails the pin.

**Do not remove `package.json` or `package-lock.json` from this fixture** even
though they are unrelated to the GitHub Actions detector fixture they sit
alongside. The B-7 workflow, gate registry, and local pre-PR selector all watch
`tests/fixtures/ecosystems/**`, so a fixture-only edit here automatically
re-runs the gate that catches a broken config-only pin.
