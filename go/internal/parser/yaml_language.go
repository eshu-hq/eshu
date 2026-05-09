package parser

import yamlparser "github.com/eshu-hq/eshu/go/internal/parser/yaml"

func (e *Engine) parseYAML(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return yamlparser.Parse(path, isDependency, yamlparser.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
