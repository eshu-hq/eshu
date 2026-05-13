package elbv2

import "strings"

func actionMaps(actions []Action) []map[string]any {
	if len(actions) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		output = append(output, actionMap(action))
	}
	return output
}

func actionMap(action Action) map[string]any {
	value := map[string]any{
		"forward_target_groups": weightedTargetGroupMaps(action.ForwardTargetGroups),
		"order":                 action.Order,
		"target_group_arn":      strings.TrimSpace(action.TargetGroupARN),
		"type":                  strings.TrimSpace(action.Type),
	}
	if action.Redirect != nil {
		value["redirect"] = map[string]any{
			"host":        strings.TrimSpace(action.Redirect.Host),
			"path":        strings.TrimSpace(action.Redirect.Path),
			"port":        strings.TrimSpace(action.Redirect.Port),
			"protocol":    strings.TrimSpace(action.Redirect.Protocol),
			"query":       strings.TrimSpace(action.Redirect.Query),
			"status_code": strings.TrimSpace(action.Redirect.StatusCode),
		}
	}
	if action.FixedResponse != nil {
		value["fixed_response"] = map[string]any{
			"content_type": strings.TrimSpace(action.FixedResponse.ContentType),
			"message_body": strings.TrimSpace(action.FixedResponse.MessageBody),
			"status_code":  strings.TrimSpace(action.FixedResponse.StatusCode),
		}
	}
	return value
}

func weightedTargetGroupMaps(groups []WeightedTargetGroup) []map[string]any {
	if len(groups) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		output = append(output, map[string]any{
			"target_group_arn": strings.TrimSpace(group.ARN),
			"weight":           group.Weight,
		})
	}
	return output
}

func conditionMaps(conditions []Condition) []map[string]any {
	if len(conditions) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(conditions))
	for _, condition := range conditions {
		output = append(output, map[string]any{
			"field":                strings.TrimSpace(condition.Field),
			"host_header_values":   cloneStrings(condition.HostHeaderValues),
			"http_header_name":     strings.TrimSpace(condition.HTTPHeaderName),
			"http_header_values":   cloneStrings(condition.HTTPHeaderValues),
			"http_request_methods": cloneStrings(condition.HTTPRequestMethods),
			"path_pattern_values":  cloneStrings(condition.PathPatternValues),
			"query_strings":        queryStringConditionMaps(condition.QueryStrings),
			"source_ip_values":     cloneStrings(condition.SourceIPValues),
			"values":               cloneStrings(condition.Values),
		})
	}
	return output
}

func queryStringConditionMaps(conditions []QueryStringCondition) []map[string]string {
	if len(conditions) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(conditions))
	for _, condition := range conditions {
		output = append(output, map[string]string{
			"key":   strings.TrimSpace(condition.Key),
			"value": strings.TrimSpace(condition.Value),
		})
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
