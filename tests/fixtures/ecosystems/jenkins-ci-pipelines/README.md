# jenkins-ci-pipelines

B-7 corpus fixture. A Jenkins CI pipeline repository whose `Jenkinsfile` checks
out the in-corpus `deployable-source` repository via an explicit
`git url: 'https://github.com/acme/deployable-source'` step. The relationships
engine emits `JENKINS_GITHUB_REPOSITORY` evidence resolving to `deployable-source`,
materialising a `(:Repository)-[:DISCOVERS_CONFIG_IN]->(:Repository)` edge.

The golden-corpus gate asserts this edge filtered on
`evidence_kinds=[JENKINS_GITHUB_REPOSITORY]`, isolating the Jenkins
config-discovery verb (`DISCOVERS_CONFIG_IN` is also emitted by Terraform,
Terragrunt and GitHub Actions).

## Groovy pipeline-entrypoint dead-code root coverage & Ifá determination

Separately from the `DISCOVERS_CONFIG_IN` edge above, `src/DeployHelper.groovy`
is a foil for the `groovy.jenkins_pipeline_entrypoint` dead-code root
(#5337, #5378): it is an ordinary Groovy source file (not a `Jenkinsfile`, not
under `vars/`), so its `deployApp` method — though it calls `pipelineDeploy(` —
gets no `dead_code_root_kind`. The B-12 snapshot (HTTP query shape
`POST /api/v0/code/dead-code/investigate?golden_scope=jenkins-ci-pipelines`)
pins this live: `deployApp` (foil) must appear in the `ambiguous` bucket with
classification `derived_candidate_only`, and `Jenkinsfile` (the rooted pipeline
entrypoint) must appear in `suppressed` with classification `excluded`. Both
object-matches are closed on `(name, classification)`. Parser-tier proof:
`TestJenkinsGroovyGoldenFixtureDiscriminatesPipeline`.

Ifá materialized-edge coverage for this **dead-code-root** discrimination is
**N/A**: the pipeline-entrypoint root is a content-store dead-code verdict; it
writes no reducer/graph edge and adds no `reducer.MaterializedEdgeFamilies()`
domain. (The `DISCOVERS_CONFIG_IN` config-discovery edge above is a separate
detector.)

## Second pipeline and shared-library structure (#5569)

The `Jenkinsfile` and `src/DeployHelper.groovy` above are byte-for-byte
load-bearing for the golden discrimination and are never edited to add
richness. Corpus-quality enrichment instead adds a second pipeline plus a
shared-library `vars/` directory, so the fixture reads as more than a
single-pipeline repository:

- `pipelines/release/Jenkinsfile` — a second, nested pipeline. `isJenkinsfile`
  matches on basename only, so this is rooted as its own
  `groovy.jenkins_pipeline_entrypoint` exactly like the root `Jenkinsfile`,
  proving the root is basename-driven rather than path-driven. It calls the
  `pipelineRelease(...)` global step as a real Jenkins controller would
  (library configured server-side, not declared per-Jenkinsfile via
  `@Library(...)`), so no `JENKINS_SHARED_LIBRARY` catalog-matching evidence
  is emitted by this file.
- `vars/pipelineRelease.groovy` — the shared-library step backing that call.
  `isSharedLibraryVarsFile` roots its `call(...)` entrypoint as
  `groovy.shared_library_call`, a dead-code root shape this fixture
  previously did not exercise.

Neither addition references `deployApp` or any other symbol from the pinned
discrimination pair above, so they add new, independently-classified
dead-code candidates without moving `Jenkinsfile`/`deployApp` between
buckets, and neither resolves against the `deployable-source` catalog entry
used by the `DISCOVERS_CONFIG_IN` assertion above.

Trap for future edits to any `.groovy`/`Jenkinsfile` source in this fixture:
`groovyLibraryStepPattern` in `go/internal/parser/groovy/metadata.go` matches
the word `library` against raw source text (comments included) and then
captures everything up to the *next* quote character in the file, unbounded
across lines. A comment that puts a quote character right after the word
`library` (for example a quoted phrase ending in `..."library"`) turns
everything up to the following quote elsewhere in the file into fabricated
`shared_libraries` evidence. `pipelines/release/Jenkinsfile` deliberately
avoids any quote character in its comment block for this reason.

No proprietary data: all identifiers are synthetic (`acme` org).
