package cloudformation

func evaluateAnd(
	args []any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
	visiting map[string]bool,
) (bool, bool) {
	for _, arg := range args {
		value, resolved := evaluateConditionValue(
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

func evaluateOr(
	args []any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
	visiting map[string]bool,
) (bool, bool) {
	for _, arg := range args {
		value, resolved := evaluateConditionValue(
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

func resolveComparable(
	value any,
	conditions map[string]any,
	defaults map[string]any,
	evaluations map[string]conditionEvaluation,
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
			return evaluateConditionByName(
				conditionName, conditions, defaults, evaluations, visiting,
			)
		}
	}

	return nil, false
}
