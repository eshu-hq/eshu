package groovy

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Parse builds the parent parser payload for a Groovy or Jenkinsfile source.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	sourceBytes, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	sourceText := string(sourceBytes)
	payload := shared.BasePayload(path, "groovy", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}

	for key, value := range PipelineMetadata(sourceText).Map() {
		payload[key] = value
	}
	if options.IndexSource {
		payload["source"] = sourceText
	}
	return payload, nil
}

// PreScan returns deterministic Groovy metadata names for repository pre-scan
// import maps.
func PreScan(path string) ([]string, error) {
	sourceBytes, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	metadata := PipelineMetadata(string(sourceBytes))
	names := make([]string, 0)
	for _, values := range [][]string{
		metadata.SharedLibraries,
		metadata.PipelineCalls,
		metadata.EntryPoints,
	} {
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
