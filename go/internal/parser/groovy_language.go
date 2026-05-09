package parser

import (
	groovyparser "github.com/eshu-hq/eshu/go/internal/parser/groovy"
)

func (e *Engine) parseGroovy(path string, isDependency bool, options Options) (map[string]any, error) {
	return groovyparser.Parse(path, isDependency, sharedOptions(options))
}

func (e *Engine) preScanGroovy(path string) ([]string, error) {
	return groovyparser.PreScan(path)
}

// ExtractGroovyPipelineMetadata returns the explicit Jenkins/Groovy signals
// that the parser can safely prove from source text.
func ExtractGroovyPipelineMetadata(sourceText string) map[string]any {
	return extractGroovyPipelineMetadata(sourceText)
}

func extractGroovyPipelineMetadata(sourceText string) map[string]any {
	return groovyparser.PipelineMetadata(sourceText).Map()
}
