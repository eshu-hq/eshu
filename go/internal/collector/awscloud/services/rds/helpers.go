package rds

import "strings"

func parameterGroupMaps(groups []ParameterGroup) []map[string]any {
	if len(groups) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		output = append(output, map[string]any{
			"name":         name,
			"apply_status": strings.TrimSpace(group.State),
		})
	}
	return output
}

func optionGroupMaps(groups []OptionGroup) []map[string]any {
	if len(groups) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		output = append(output, map[string]any{
			"name":   name,
			"status": strings.TrimSpace(group.State),
		})
	}
	return output
}

func clusterMemberIDs(members []ClusterMember) []string {
	if len(members) == 0 {
		return nil
	}
	output := make([]string, 0, len(members))
	for _, member := range members {
		if identifier := strings.TrimSpace(member.DBInstanceIdentifier); identifier != "" {
			output = append(output, identifier)
		}
	}
	return output
}

func writerInstanceIDs(members []ClusterMember) []string {
	if len(members) == 0 {
		return nil
	}
	output := make([]string, 0, len(members))
	for _, member := range members {
		if !member.IsWriter {
			continue
		}
		if identifier := strings.TrimSpace(member.DBInstanceIdentifier); identifier != "" {
			output = append(output, identifier)
		}
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
