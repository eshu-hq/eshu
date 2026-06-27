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

No proprietary data: all identifiers are synthetic (`acme` org).
