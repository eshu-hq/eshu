package parser

import (
	javaparser "github.com/eshu-hq/eshu/go/internal/parser/java"
)

func parseJavaMetadata(path string, isDependency bool) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	payload := basePayload(path, "java_metadata", isDependency)
	for _, ref := range javaparser.MetadataClassReferences(path, string(source)) {
		appendBucket(payload, "function_calls", map[string]any{
			"name":             ref.Name,
			"full_name":        ref.FullName,
			"line_number":      ref.LineNumber,
			"lang":             "java_metadata",
			"call_kind":        ref.Kind,
			"referenced_class": ref.FullName,
		})
	}
	sortNamedBucket(payload, "function_calls")
	return payload, nil
}
