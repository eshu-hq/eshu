package collector

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnySlice(input []any) []any {
	if input == nil {
		return nil
	}

	cloned := make([]any, len(input))
	for i, value := range input {
		cloned[i] = cloneAnyValue(value)
	}
	return cloned
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	return append([]string(nil), input...)
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}

var snapshotEntityReservedKeys = map[string]struct{}{
	"artifact_type":    {},
	"deleted":          {},
	"end_byte":         {},
	"end_line":         {},
	"iac_relevant":     {},
	"lang":             {},
	"language":         {},
	"line_number":      {},
	"name":             {},
	"source":           {},
	"start_byte":       {},
	"template_dialect": {},
	"uid":              {},
}

func snapshotEntityMetadata(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}

	metadata := make(map[string]any)
	for key, value := range payload {
		if _, reserved := snapshotEntityReservedKeys[key]; reserved {
			continue
		}
		metadata[key] = cloneAnyValue(value)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
