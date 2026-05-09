package parser

import (
	"slices"
	"strings"

	groovyparser "github.com/eshu-hq/eshu/go/internal/parser/groovy"
)

func (e *Engine) parseGroovy(path string, isDependency bool, options Options) (map[string]any, error) {
	sourceBytes, err := readSource(path)
	if err != nil {
		return nil, err
	}

	sourceText := string(sourceBytes)
	payload := basePayload(path, "groovy", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}

	metadata := extractGroovyPipelineMetadata(sourceText)
	for key, value := range metadata {
		payload[key] = value
	}
	if options.IndexSource {
		payload["source"] = sourceText
	}
	return payload, nil
}

func (e *Engine) preScanGroovy(path string) ([]string, error) {
	sourceBytes, err := readSource(path)
	if err != nil {
		return nil, err
	}

	metadata := extractGroovyPipelineMetadata(string(sourceBytes))
	names := make([]string, 0)
	for _, key := range []string{"shared_libraries", "pipeline_calls", "entry_points"} {
		values, ok := metadata[key].([]string)
		if !ok {
			continue
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if slices.Contains(names, value) {
				continue
			}
			names = append(names, value)
		}
	}
	slices.Sort(names)
	return names, nil
}

// ExtractGroovyPipelineMetadata returns the explicit Jenkins/Groovy signals
// that the parser can safely prove from source text.
func ExtractGroovyPipelineMetadata(sourceText string) map[string]any {
	return extractGroovyPipelineMetadata(sourceText)
}

func extractGroovyPipelineMetadata(sourceText string) map[string]any {
	return groovyparser.PipelineMetadata(sourceText).Map()
}
