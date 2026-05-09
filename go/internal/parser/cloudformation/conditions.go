package cloudformation

import (
	"fmt"
	"strings"
)

type conditionEvaluation struct {
	Resolved bool
	Value    bool
}

func evaluateConditions(document map[string]any) map[string]conditionEvaluation {
	conditions, ok := document["Conditions"].(map[string]any)
	if !ok || len(conditions) == 0 {
		return nil
	}

	defaults := parameterDefaults(document)
	evaluations := make(map[string]conditionEvaluation, len(conditions))
	visiting := make(map[string]bool, len(conditions))
	for name := range conditions {
		if value, resolved := evaluateConditionByName(name, conditions, defaults, evaluations, visiting); resolved {
			evaluations[name] = conditionEvaluation{
				Resolved: true,
				Value:    value,
			}
		}
	}

	return evaluations
}

func parameterDefaults(document map[string]any) map[string]any {
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

func evaluateConditionByName(
	name string,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
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
	value, resolved := evaluateConditionValue(expression, conditions, defaults, evaluations, visiting)
	delete(visiting, name)
	if !resolved {
		return false, false
	}

	evaluations[name] = conditionEvaluation{
		Resolved: true,
		Value:    value,
	}
	return value, true
}

func evaluateConditionValue(
	expression any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
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
		return evaluateConditionMap(typed, conditions, defaults, evaluations, visiting)
	}

	return false, false
}

func evaluateConditionMap(
	typed map[string]any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
	visiting map[string]bool,
) (bool, bool) {
	if conditionName, ok := typed["Condition"].(string); ok {
		return evaluateConditionByName(
			conditionName, conditions, defaults, evaluations, visiting,
		)
	}
	if args, ok := typed["Fn::Equals"].([]any); ok && len(args) == 2 {
		left, leftOK := resolveComparable(args[0], conditions, defaults, evaluations, visiting)
		right, rightOK := resolveComparable(args[1], conditions, defaults, evaluations, visiting)
		if !leftOK || !rightOK {
			return false, false
		}
		return fmt.Sprint(left) == fmt.Sprint(right), true
	}
	if args, ok := typed["Fn::And"].([]any); ok && len(args) > 0 {
		return evaluateAnd(args, conditions, defaults, evaluations, visiting)
	}
	if args, ok := typed["Fn::Or"].([]any); ok && len(args) > 0 {
		return evaluateOr(args, conditions, defaults, evaluations, visiting)
	}
	if args, ok := typed["Fn::Not"].([]any); ok && len(args) == 1 {
		value, resolved := evaluateConditionValue(
			args[0], conditions, defaults, evaluations, visiting,
		)
		if !resolved {
			return false, false
		}
		return !value, true
	}
	return false, false
}
