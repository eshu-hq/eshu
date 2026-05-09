package java

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// ParseMetadata reads a Java metadata file and emits class-reference calls
// using the parent parser payload shape.
func ParseMetadata(path string, isDependency bool) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := shared.BasePayload(path, "java_metadata", isDependency)
	for _, ref := range MetadataClassReferences(path, string(source)) {
		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":             ref.Name,
			"full_name":        ref.FullName,
			"line_number":      ref.LineNumber,
			"lang":             "java_metadata",
			"call_kind":        ref.Kind,
			"referenced_class": ref.FullName,
		})
	}
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}
