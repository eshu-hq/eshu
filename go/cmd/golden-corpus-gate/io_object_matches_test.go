package main

import "strings"

func fakeSetJSONObjectMatches(root map[string]any, path string, matches []map[string]any) {
	segments := strings.Split(path, ".")
	var current any = root
	for i, rawSegment := range segments {
		last := i == len(segments)-1
		arraySegment := strings.HasSuffix(rawSegment, "[]")
		segment := strings.TrimSuffix(rawSegment, "[]")
		obj, ok := current.(map[string]any)
		if !ok || segment == "" {
			return
		}
		if last {
			if !arraySegment {
				return
			}
			existing, _ := obj[segment].([]any)
			items := make([]any, 0, len(matches))
			for index, match := range matches {
				item := map[string]any{}
				if index < len(existing) {
					if existingObject, ok := existing[index].(map[string]any); ok {
						for key, value := range existingObject {
							item[key] = value
						}
					}
				}
				for key, value := range match {
					item[key] = value
				}
				items = append(items, item)
			}
			obj[segment] = items
			return
		}
		if arraySegment {
			arr, _ := obj[segment].([]any)
			if len(arr) == 0 {
				arr = []any{map[string]any{}}
				obj[segment] = arr
			}
			current = arr[0]
			continue
		}
		next, _ := obj[segment].(map[string]any)
		if next == nil {
			next = map[string]any{}
			obj[segment] = next
		}
		current = next
	}
}
