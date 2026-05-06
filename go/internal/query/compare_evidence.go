package query

import "strings"

func inferredEnvironmentProvenance(environment string, evidence ServiceQueryEvidence) []map[string]any {
	canonical := canonicalEnvironmentName(environment)
	if canonical == "" {
		canonical = strings.ToLower(strings.TrimSpace(environment))
	}

	provenance := make([]map[string]any, 0)
	for _, row := range evidence.Environments {
		if canonicalEnvironmentName(row.Environment) != canonical {
			continue
		}
		provenance = append(provenance, map[string]any{
			"kind":          "service_environment_evidence",
			"source":        "content",
			"environment":   row.Environment,
			"relative_path": row.RelativePath,
			"reason":        row.Reason,
			"value":         row.Environment,
		})
	}
	for _, row := range evidence.Hostnames {
		if canonicalEnvironmentName(row.Environment) != canonical {
			continue
		}
		provenance = append(provenance, map[string]any{
			"kind":          "service_hostname_evidence",
			"source":        "content",
			"environment":   row.Environment,
			"relative_path": row.RelativePath,
			"reason":        row.Reason,
			"value":         row.Hostname,
		})
	}
	return provenance
}

func canonicalEnvironmentName(environment string) string {
	normalized := strings.ToLower(strings.TrimSpace(environment))
	if normalized == "" {
		return ""
	}
	for _, alias := range environmentAliases {
		for _, candidate := range alias.aliases {
			if normalized == candidate {
				return alias.canonical
			}
		}
	}
	return normalized
}
