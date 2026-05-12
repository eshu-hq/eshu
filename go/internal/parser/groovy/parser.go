package groovy

import (
	"path/filepath"
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
	for _, class := range ExtractClassEntities(sourceText) {
		shared.AppendBucket(payload, "classes", class)
	}
	for _, function := range ExtractFunctionEntities(path, sourceText) {
		shared.AppendBucket(payload, "functions", function)
	}
	for _, call := range ExtractFunctionCallEntities(sourceText) {
		shared.AppendBucket(payload, "function_calls", call)
	}
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "function_calls")
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

func isSharedLibraryVarsFile(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	return (strings.HasPrefix(normalized, "vars/") || strings.Contains(normalized, "/vars/")) &&
		strings.HasSuffix(strings.ToLower(normalized), ".groovy")
}
