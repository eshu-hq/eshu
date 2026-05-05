package parser

import "strings"

func parsePHPClassTraitAdaptations(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if !strings.Contains(trimmed, "insteadof") && !strings.Contains(trimmed, " as ") {
		return nil
	}

	body := trimmed
	if strings.HasPrefix(body, "use ") {
		body = strings.TrimSpace(body[len("use "):])
	}
	if openBrace := strings.Index(body, "{"); openBrace >= 0 {
		if closeBrace := strings.LastIndex(body, "}"); closeBrace > openBrace {
			body = body[openBrace+1 : closeBrace]
		}
	}
	clauses := strings.Split(body, ";")
	adaptations := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		if !strings.Contains(clause, "insteadof") && !strings.Contains(clause, " as ") {
			continue
		}
		adaptations = append(adaptations, clause)
	}
	return dedupeNonEmptyStrings(adaptations)
}

func appendPHPClassTraitAdaptations(payload map[string]any, className string, additionalAdaptations []string) {
	if className == "" || len(additionalAdaptations) == 0 {
		return
	}
	items, _ := payload["classes"].([]map[string]any)
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != className {
			continue
		}
		existing, _ := item["trait_adaptations"].([]string)
		merged := dedupeNonEmptyStrings(append(existing, additionalAdaptations...))
		if len(merged) > 0 {
			item["trait_adaptations"] = merged
		}
		return
	}
}
