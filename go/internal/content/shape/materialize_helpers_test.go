package shape

type EntityRecordExpectation struct {
	entityType  string
	entityName  string
	startLine   int
	endLine     int
	sourceCache string
	entityID    string
}

func boolPtr(value bool) *bool {
	return &value
}

func toStringSlice(value any) []string {
	items, ok := value.([]string)
	if ok {
		return items
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	converted := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		converted = append(converted, text)
	}
	return converted
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
