// FOIL (#5337): an ordinary src/*.groovy source file. It is NOT a Jenkins
// artifact (not a Jenkinsfile, not under vars/), so the Jenkins PipelineMetadata
// extraction is gated OFF for it even though deployApp calls `pipelineDeploy(`,
// which matches the pipeline-call regex. The genuine AST call still lands in
// function_calls, but pipeline_calls / shared_libraries evidence is withheld,
// and deployApp gets no dead_code_root_kind — the discriminating opposite of the
// Jenkinsfile pipeline entrypoint, which IS rooted as
// groovy.jenkins_pipeline_entrypoint.
class DeployHelper {
    def deployApp(String target) {
        pipelineDeploy(target)
    }
}
