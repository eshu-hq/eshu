package parser

import (
	"fmt"
	"strings"
)

type cloudFormationConditionEvaluation struct {
	Resolved bool
	Value    bool
}

func evaluateCloudFormationConditions(document map[string]any) map[string]cloudFormationConditionEvaluation {
	conditions, ok := document["Conditions"].(map[string]any)
	if !ok || len(conditions) == 0 {
		return nil
	}

	defaults := cloudFormationParameterDefaults(document)
	evaluations := make(map[string]cloudFormationConditionEvaluation, len(conditions))
	visiting := make(map[string]bool, len(conditions))
	for name := range conditions {
		if value, resolved := evaluateCloudFormationConditionByName(name, conditions, defaults, evaluations, visiting); resolved {
			evaluations[name] = cloudFormationConditionEvaluation{
				Resolved: true,
				Value:    value,
			}
		}
	}

	return evaluations
}

func cloudFormationParameterDefaults(document map[string]any) map[string]any {
	parameters, ok := document["Parameters"].(map[string]any)
	if !ok || len(parameters) == 0 {
		return nil
	}

	defaults := make(map[string]any, len(parameters))
	for name, raw := range parameters {
		body, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := body["Default"]; ok {
			defaults[name] = value
		}
	}
	return defaults
}

func evaluateCloudFormationConditionByName(
	name string,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]cloudFormationConditionEvaluation,
	visiting map[string]bool,
) (bool, bool) {
	if evaluation, ok := evaluations[name]; ok && evaluation.Resolved {
		return evaluation.Value, true
	}
	if visiting[name] {
		return false, false
	}

	expression, ok := conditions[name]
	if !ok {
		return false, false
	}

	visiting[name] = true
	value, resolved := evaluateCloudFormationConditionValue(expression, conditions, defaults, evaluations, visiting)
	delete(visiting, name)
	if !resolved {
		return false, false
	}

	evaluations[name] = cloudFormationConditionEvaluation{
		Resolved: true,
		Value:    value,
	}
	return value, true
}

func evaluateCloudFormationConditionValue(
	expression any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]cloudFormationConditionEvaluation,
	visiting map[string]bool,
) (bool, bool) {
	switch typed := expression.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	case map[string]any:
		if conditionName, ok := typed["Condition"].(string); ok {
			return evaluateCloudFormationConditionByName(
				conditionName, conditions, defaults, evaluations, visiting,
			)
		}
		if args, ok := typed["Fn::Equals"].([]any); ok && len(args) == 2 {
			left, leftOK := resolveCloudFormationComparable(args[0], conditions, defaults, evaluations, visiting)
			right, rightOK := resolveCloudFormationComparable(args[1], conditions, defaults, evaluations, visiting)
			if !leftOK || !rightOK {
				return false, false
			}
			return fmt.Sprint(left) == fmt.Sprint(right), true
		}
		if args, ok := typed["Fn::And"].([]any); ok && len(args) > 0 {
			for _, arg := range args {
				value, resolved := evaluateCloudFormationConditionValue(
					arg, conditions, defaults, evaluations, visiting,
				)
				if !resolved {
					return false, false
				}
				if !value {
					return false, true
				}
			}
			return true, true
		}
		if args, ok := typed["Fn::Or"].([]any); ok && len(args) > 0 {
			for _, arg := range args {
				value, resolved := evaluateCloudFormationConditionValue(
					arg, conditions, defaults, evaluations, visiting,
				)
				if !resolved {
					return false, false
				}
				if value {
					return true, true
				}
			}
			return false, true
		}
		if args, ok := typed["Fn::Not"].([]any); ok && len(args) == 1 {
			value, resolved := evaluateCloudFormationConditionValue(
				args[0], conditions, defaults, evaluations, visiting,
			)
			if !resolved {
				return false, false
			}
			return !value, true
		}
	}

	return false, false
}

func resolveCloudFormationComparable(
	value any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]cloudFormationConditionEvaluation,
	visiting map[string]bool,
) (any, bool) {
	switch typed := value.(type) {
	case string, bool, int, int32, int64, float32, float64:
		return typed, true
	case map[string]any:
		if refName, ok := typed["Ref"].(string); ok {
			resolved, ok := defaults[refName]
			return resolved, ok
		}
		if conditionName, ok := typed["Condition"].(string); ok {
			return evaluateCloudFormationConditionByName(
				conditionName, conditions, defaults, evaluations, visiting,
			)
		}
	}

	return nil, false
}
