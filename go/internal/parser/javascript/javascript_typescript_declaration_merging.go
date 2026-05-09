package javascript

import "slices"

type declarationMergeParticipant struct {
	item map[string]any
	kind string
}

func annotateTypeScriptDeclarationMerges(payload map[string]any, outputLanguage string) {
	if outputLanguage == "javascript" || payload == nil {
		return
	}

	groups := make(map[string][]declarationMergeParticipant)
	appendGroup := func(bucketKey string, kind string) {
		items, ok := payload[bucketKey].([]map[string]any)
		if !ok {
			return
		}
		for i := range items {
			name, _ := items[i]["name"].(string)
			if name == "" {
				continue
			}
			groups[name] = append(groups[name], declarationMergeParticipant{item: items[i], kind: kind})
		}
	}

	appendGroup("functions", "function")
	appendGroup("classes", "class")
	appendGroup("interfaces", "interface")
	appendGroup("modules", "namespace")
	appendGroup("enums", "enum")

	for name, participants := range groups {
		if len(participants) < 2 {
			continue
		}

		kinds := declarationMergeKinds(participants)
		if len(kinds) == 0 {
			continue
		}

		for _, participant := range participants {
			participant.item["declaration_merge_group"] = name
			participant.item["declaration_merge_count"] = len(participants)
			participant.item["declaration_merge_kinds"] = append([]string(nil), kinds...)
		}
	}
}

func declarationMergeKinds(participants []declarationMergeParticipant) []string {
	kinds := make([]string, 0, len(participants))
	for _, participant := range participants {
		if participant.kind == "" {
			continue
		}
		if slices.Contains(kinds, participant.kind) {
			continue
		}
		kinds = append(kinds, participant.kind)
	}
	return kinds
}
