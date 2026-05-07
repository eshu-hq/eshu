class DynamicGroovyFixture {
  static void main(String[] args) {
    directGroovyHelper()
    selectedGroovyHandler.call()
    pipelineRoot()
  }

  static void unusedGroovyHelper() {}

  static void directGroovyHelper() {}

  static void publicGroovyApi() {}

  static void pipelineRoot() {
    stage('build') {
      directGroovyHelper()
    }
  }

  static Closure selectedGroovyHandler = { directGroovyHelper() }

  static void generatedGroovyStub() {}

  static void dynamicGroovyDispatch(String name) {
    "$name"()
  }
}
