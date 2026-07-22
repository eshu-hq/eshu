// Shared-library global step called by pipelines/release/Jenkinsfile's
// pipelineRelease(...) invocation (#5569). Real Jenkins shared libraries
// expose vars/<stepName>.groovy files with a top-level call(...)
// entrypoint; go/internal/parser/groovy/parser.go's isSharedLibraryVarsFile
// gates PipelineMetadata extraction and the groovy.shared_library_call
// dead-code root onto this exact vars/ layout, so this file exercises that
// shape live in the corpus (previously untested by this fixture, which only
// carried Jenkinsfile-entrypoint and ordinary-source-file evidence).
def call(Map config = [:]) {
    echo "Releasing version ${config.version}"
    sh "./scripts/release.sh ${config.version}"
}
