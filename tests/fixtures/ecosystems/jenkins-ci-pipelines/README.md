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

No proprietary data: all identifiers are synthetic (`acme` org).
