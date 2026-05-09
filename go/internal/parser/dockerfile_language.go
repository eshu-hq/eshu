package parser

import dockerfileparser "github.com/eshu-hq/eshu/go/internal/parser/dockerfile"

func (e *Engine) parseDockerfile(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "dockerfile", isDependency)
	for key, value := range buildDockerfilePayload(string(source)) {
		payload[key] = value
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

// ExtractDockerfileRuntimeMetadata returns the parser-backed Dockerfile payload
// without repository-specific metadata so read-side query code can surface the
// same stage/runtime signals the parser already proves during ingestion.
func ExtractDockerfileRuntimeMetadata(sourceText string) map[string]any {
	return buildDockerfilePayload(sourceText)
}

func buildDockerfilePayload(sourceText string) map[string]any {
	return dockerfileparser.RuntimeMetadata(sourceText).Map()
}
