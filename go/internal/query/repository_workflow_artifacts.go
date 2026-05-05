package query

import (
	"fmt"
	"sort"
	"strings"
)

func enrichWorkflowArtifactRow(row map[string]any, content string) {
	reusableWorkflowRepositories,
		localReusableWorkflowPaths,
		checkoutRepositories,
		actionRepositories,
		workflowInputRepositories,
		runCommands,
		gatingConditions,
		needsDependencies,
		triggerEvents,
		workflowInputs,
		permissionScopes,
		concurrencyGroups,
		environments,
		jobTimeoutMinutes,
		matrixKeys,
		matrixCombinationCount := workflowArtifactDetails(content)
	signals := stringSliceValue(row, "signals")

	if len(reusableWorkflowRepositories) > 0 {
		row["reusable_workflow_repositories"] = reusableWorkflowRepositories
		signals = append(signals, "reusable_workflow_refs")
	}
	if len(localReusableWorkflowPaths) > 0 {
		row["local_reusable_workflow_paths"] = localReusableWorkflowPaths
		signals = append(signals, "local_reusable_workflow_refs")
	}
	if len(checkoutRepositories) > 0 {
		row["checkout_repositories"] = checkoutRepositories
		signals = append(signals, "checkout_repositories")
	}
	if len(actionRepositories) > 0 {
		row["action_repositories"] = actionRepositories
		signals = append(signals, "action_repositories")
	}
	if len(workflowInputRepositories) > 0 {
		row["workflow_input_repositories"] = workflowInputRepositories
		signals = append(signals, "workflow_input_repositories")
	}
	if len(runCommands) > 0 {
		row["run_commands"] = runCommands
		row["command_count"] = len(runCommands)
		signals = append(signals, "run_commands")
	}
	if len(gatingConditions) > 0 {
		row["gating_conditions"] = gatingConditions
		signals = append(signals, "gating_conditions")
	}
	if len(needsDependencies) > 0 {
		row["needs_dependencies"] = needsDependencies
		signals = append(signals, "job_dependencies")
	}
	if len(triggerEvents) > 0 {
		row["trigger_events"] = triggerEvents
		signals = append(signals, "workflow_triggers")
	}
	if deliveryCommandFamilies := workflowDeliveryCommandFamilies(runCommands); len(deliveryCommandFamilies) > 0 {
		row["delivery_command_families"] = deliveryCommandFamilies
		signals = append(signals, "delivery_command_families")
	}
	if deliveryLocalPaths := workflowDeliveryLocalPaths(runCommands); len(deliveryLocalPaths) > 0 {
		row["delivery_local_paths"] = deliveryLocalPaths
		signals = append(signals, "delivery_local_paths")
	}
	if len(workflowInputs) > 0 {
		row["workflow_inputs"] = workflowInputs
	}
	if len(permissionScopes) > 0 {
		row["permission_scopes"] = permissionScopes
		signals = append(signals, "workflow_permissions")
	}
	if len(concurrencyGroups) > 0 {
		row["concurrency_groups"] = concurrencyGroups
		signals = append(signals, "workflow_concurrency")
	}
	if len(environments) > 0 {
		row["environments"] = environments
		signals = append(signals, "workflow_environments")
	}
	if len(jobTimeoutMinutes) > 0 {
		row["job_timeout_minutes"] = jobTimeoutMinutes
		signals = append(signals, "workflow_timeouts")
	}
	if len(matrixKeys) > 0 {
		row["matrix_keys"] = matrixKeys
		signals = append(signals, "matrix_strategy")
	}
	if matrixCombinationCount > 0 {
		row["matrix_combination_count"] = matrixCombinationCount
	}
	if len(signals) > 0 {
		row["signals"] = uniqueWorkflowStringsPreserveOrder(signals)
	}
}

func workflowArtifactDetails(content string) ([]string, []string, []string, []string, []string, []string, []string, []string, []string, []string, []string, []string, []string, []string, []string, int) {
	documents, err := decodeYAMLMaps(content)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0
	}

	reusableWorkflowRepositories := make([]string, 0)
	localReusableWorkflowPaths := make([]string, 0)
	checkoutRepositories := make([]string, 0)
	actionRepositories := make([]string, 0)
	workflowInputRepositories := make([]string, 0)
	runCommands := make([]string, 0)
	gatingConditions := make([]string, 0)
	needsDependencies := make([]string, 0)
	triggerEvents := make([]string, 0)
	workflowInputs := make([]string, 0)
	permissionScopes := make([]string, 0)
	concurrencyGroups := make([]string, 0)
	environments := make([]string, 0)
	jobTimeoutMinutes := make([]string, 0)
	matrixKeys := make([]string, 0)
	matrixCombinationCount := 0
	for _, document := range documents {
		triggerEvents = append(triggerEvents, githubActionsTriggerEvents(document["on"])...)
		workflowInputs = append(workflowInputs, githubActionsWorkflowInputs(document["on"])...)
		permissionScopes = append(permissionScopes, githubActionsPermissionScopes(document["permissions"])...)
		concurrencyGroups = append(concurrencyGroups, githubActionsConcurrencyGroups(document["concurrency"])...)
		jobs, ok := document["jobs"].(map[string]any)
		if !ok {
			continue
		}
		for jobName, rawJob := range jobs {
			job, ok := rawJob.(map[string]any)
			if !ok {
				continue
			}
			if workflowRef := githubActionsReusableWorkflowRepoRef(StringVal(job, "uses")); workflowRef != "" {
				reusableWorkflowRepositories = append(reusableWorkflowRepositories, workflowRef)
			}
			if localWorkflowPath := githubActionsLocalReusableWorkflowPath(StringVal(job, "uses")); localWorkflowPath != "" {
				localReusableWorkflowPaths = append(localReusableWorkflowPaths, localWorkflowPath)
			}
			permissionScopes = append(permissionScopes, githubActionsPermissionScopes(job["permissions"])...)
			concurrencyGroups = append(concurrencyGroups, githubActionsConcurrencyGroups(job["concurrency"])...)
			environments = append(environments, githubActionsEnvironmentNames(job["environment"])...)
			jobTimeoutMinutes = append(jobTimeoutMinutes, githubActionsJobTimeoutMetadata(jobName, job["timeout-minutes"])...)
			workflowInputRepositories = append(
				workflowInputRepositories,
				githubActionsWorkflowInputRepositoryMetadata(job)...,
			)
			if with, ok := job["with"].(map[string]any); ok {
				workflowInputRepositories = append(
					workflowInputRepositories,
					githubActionsWorkflowInputRepositoryMetadata(with)...,
				)
			}
			if condition := strings.TrimSpace(StringVal(job, "if")); condition != "" {
				gatingConditions = append(gatingConditions, "job "+jobName+" if "+condition)
			}
			needsDependencies = append(needsDependencies, githubActionsNeedsDependencies(jobName, job["needs"])...)
			jobMatrixKeys, jobMatrixCombinationCount := githubActionsMatrixMetadata(job["strategy"])
			matrixKeys = append(matrixKeys, jobMatrixKeys...)
			matrixCombinationCount += jobMatrixCombinationCount
			steps, ok := job["steps"].([]any)
			if !ok {
				continue
			}
			for _, rawStep := range steps {
				step, ok := rawStep.(map[string]any)
				if !ok {
					continue
				}
				if usesValue := StringVal(step, "uses"); strings.HasPrefix(strings.TrimSpace(usesValue), "actions/checkout@") {
					checkoutRepositories = append(checkoutRepositories, githubActionsCheckoutRepositories(step)...)
				}
				if actionRepository := githubActionsActionRepositoryRef(StringVal(step, "uses")); actionRepository != "" {
					actionRepositories = append(actionRepositories, actionRepository)
				}
				runCommand := strings.TrimSpace(StringVal(step, "run"))
				if condition := strings.TrimSpace(StringVal(step, "if")); condition != "" {
					gatingConditions = append(
						gatingConditions,
						"step "+jobName+"/"+workflowStepName(step)+" if "+condition,
					)
				}
				if runCommand == "" {
					continue
				}
				runCommands = append(runCommands, runCommand)
			}
		}
	}

	return sortedUniqueWorkflowStrings(reusableWorkflowRepositories),
		sortedUniqueWorkflowStrings(localReusableWorkflowPaths),
		sortedUniqueWorkflowStrings(checkoutRepositories),
		sortedUniqueWorkflowStrings(actionRepositories),
		sortedUniqueWorkflowStrings(workflowInputRepositories),
		sortedUniqueWorkflowStrings(runCommands),
		sortedUniqueWorkflowStrings(gatingConditions),
		sortedUniqueWorkflowStrings(needsDependencies),
		sortedUniqueWorkflowStrings(triggerEvents),
		sortedUniqueWorkflowStrings(workflowInputs),
		sortedUniqueWorkflowStrings(permissionScopes),
		sortedUniqueWorkflowStrings(concurrencyGroups),
		sortedUniqueWorkflowStrings(environments),
		sortedUniqueWorkflowStrings(jobTimeoutMinutes),
		sortedUniqueWorkflowStrings(matrixKeys),
		matrixCombinationCount
}

func githubActionsTriggerEvents(rawOn any) []string {
	switch value := rawOn.(type) {
	case string:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return []string{trimmed}
		}
	case []any:
		events := make([]string, 0, len(value))
		for _, item := range value {
			trimmed := strings.TrimSpace(fmt.Sprint(item))
			if trimmed == "" || trimmed == "<nil>" {
				continue
			}
			events = append(events, trimmed)
		}
		return events
	case map[string]any:
		events := make([]string, 0, len(value))
		for key := range value {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" {
				continue
			}
			events = append(events, trimmed)
		}
		return events
	}
	return nil
}

func githubActionsWorkflowInputs(rawOn any) []string {
	onMap, ok := rawOn.(map[string]any)
	if !ok {
		return nil
	}
	inputs := make([]string, 0)
	for _, eventName := range []string{"workflow_dispatch", "workflow_call"} {
		event, ok := onMap[eventName].(map[string]any)
		if !ok {
			continue
		}
		eventInputs, ok := event["inputs"].(map[string]any)
		if !ok {
			continue
		}
		for key := range eventInputs {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" {
				continue
			}
			inputs = append(inputs, trimmed)
		}
	}
	return inputs
}

func githubActionsMatrixMetadata(rawStrategy any) ([]string, int) {
	strategy, ok := rawStrategy.(map[string]any)
	if !ok {
		return nil, 0
	}
	matrix, ok := strategy["matrix"].(map[string]any)
	if !ok {
		return nil, 0
	}
	keys := make([]string, 0, len(matrix))
	combinationCount := 1
	dimensionCount := 0
	for key, rawValues := range matrix {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || trimmedKey == "include" || trimmedKey == "exclude" {
			continue
		}
		values, ok := rawValues.([]any)
		if !ok || len(values) == 0 {
			continue
		}
		keys = append(keys, trimmedKey)
		dimensionCount++
		combinationCount *= len(values)
	}
	if dimensionCount == 0 {
		if include, ok := matrix["include"].([]any); ok && len(include) > 0 {
			return nil, len(include)
		}
		return nil, 0
	}
	if exclude, ok := matrix["exclude"].([]any); ok && len(exclude) > 0 && combinationCount > len(exclude) {
		combinationCount -= len(exclude)
	}
	if include, ok := matrix["include"].([]any); ok && len(include) > 0 {
		combinationCount += len(include)
	}
	return keys, combinationCount
}

func githubActionsPermissionScopes(rawPermissions any) []string {
	switch typed := rawPermissions.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case map[string]any:
		scopes := make([]string, 0, len(typed))
		for key, rawValue := range typed {
			scope := strings.TrimSpace(key)
			value := strings.TrimSpace(fmt.Sprint(rawValue))
			if scope == "" || value == "" || value == "<nil>" {
				continue
			}
			scopes = append(scopes, scope+":"+value)
		}
		return scopes
	}
	return nil
}

func githubActionsLocalReusableWorkflowPath(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if !strings.HasPrefix(trimmed, ".github/workflows/") {
		return ""
	}
	return trimmed
}

func githubActionsConcurrencyGroups(rawConcurrency any) []string {
	switch typed := rawConcurrency.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case map[string]any:
		if group := strings.TrimSpace(StringVal(typed, "group")); group != "" {
			return []string{group}
		}
	}
	return nil
}

func githubActionsEnvironmentNames(rawEnvironment any) []string {
	switch typed := rawEnvironment.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case map[string]any:
		if name := strings.TrimSpace(StringVal(typed, "name")); name != "" {
			return []string{name}
		}
	}
	return nil
}

func githubActionsJobTimeoutMetadata(jobName string, rawTimeout any) []string {
	trimmedJobName := strings.TrimSpace(jobName)
	if trimmedJobName == "" {
		return nil
	}

	var timeout string
	switch typed := rawTimeout.(type) {
	case int:
		timeout = fmt.Sprintf("%d", typed)
	case int64:
		timeout = fmt.Sprintf("%d", typed)
	case float64:
		timeout = fmt.Sprintf("%.0f", typed)
	case string:
		timeout = strings.TrimSpace(typed)
	default:
		timeout = strings.TrimSpace(fmt.Sprint(rawTimeout))
	}
	if timeout == "" || timeout == "<nil>" {
		return nil
	}
	return []string{trimmedJobName + ":" + timeout}
}

func githubActionsCheckoutRepositories(step map[string]any) []string {
	refs := make([]string, 0, 1)
	refs = append(refs, metadataStringSlice(step, "repository")...)
	if with, ok := step["with"].(map[string]any); ok {
		refs = append(refs, metadataStringSlice(with, "repository")...)
	}
	return refs
}

func githubActionsNeedsDependencies(jobName string, rawNeeds any) []string {
	needs := make([]string, 0, 2)
	switch value := rawNeeds.(type) {
	case string:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			needs = append(needs, jobName+"<-"+trimmed)
		}
	case []any:
		for _, item := range value {
			trimmed := strings.TrimSpace(StringVal(map[string]any{"needs": item}, "needs"))
			if trimmed == "" {
				continue
			}
			needs = append(needs, jobName+"<-"+trimmed)
		}
	}
	return needs
}

func workflowStepName(step map[string]any) string {
	if name := strings.TrimSpace(StringVal(step, "name")); name != "" {
		return name
	}
	if uses := strings.TrimSpace(StringVal(step, "uses")); uses != "" {
		return uses
	}
	if run := strings.TrimSpace(StringVal(step, "run")); run != "" {
		return run
	}
	return "step"
}

func sortedUniqueWorkflowStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}
	sort.Strings(filtered)
	return filtered
}

func uniqueWorkflowStringsPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}
	return filtered
}
