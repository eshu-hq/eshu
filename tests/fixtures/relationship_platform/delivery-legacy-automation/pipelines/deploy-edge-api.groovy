pipelineJob('service-edge-api-legacy') {
  definition {
    cps {
      script("""
        pipeline {
          agent any
          stages {
            stage('deploy') {
              steps {
                sh 'echo register service-edge-api legacy task'
              }
            }
          }
        }
      """.stripIndent())
      sandbox()
    }
  }
}
