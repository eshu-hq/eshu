package cigates

import "fmt"

func validateTrustedWorkflowTriggers(check RequiredStatusCheck, raw []byte) []error {
	triggers, err := workflowTriggerKeys(raw)
	if err != nil {
		return []error{fmt.Errorf("required status context %q: parse workflow triggers: %w", check.Context, err)}
	}

	var errs []error
	if _, ok := triggers["workflow_run"]; !ok {
		errs = append(errs, fmt.Errorf("required status context %q: trusted publisher must trigger on workflow_run", check.Context))
	}
	sources, sourceErr := workflowRunSources(raw)
	if sourceErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run sources: %w", check.Context, sourceErr))
	} else if !slicesContain(sources, check.SourceWorkflow) {
		errs = append(errs, fmt.Errorf(
			"required status context %q: workflow_run sources %q do not include declared source workflow %q",
			check.Context,
			sources,
			check.SourceWorkflow,
		))
	}
	types, typesErr := workflowRunList(raw, "types")
	if typesErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run activity types: %w", check.Context, typesErr))
	} else {
		for _, requiredType := range []string{"in_progress", "completed"} {
			if !slicesContain(types, requiredType) {
				errs = append(errs, fmt.Errorf(
					"required status context %q: workflow_run activity types must include %q",
					check.Context,
					requiredType,
				))
			}
		}
	}
	for _, forbidden := range []string{"pull_request", "pull_request_target"} {
		if _, ok := triggers[forbidden]; ok {
			errs = append(errs, fmt.Errorf(
				"required status context %q: trusted publisher must not execute from untrusted %s workflow code",
				check.Context,
				forbidden,
			))
		}
	}
	return errs
}
