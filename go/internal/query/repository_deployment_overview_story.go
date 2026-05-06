package query

import (
	"fmt"
	"strings"
)

func buildOverviewTopologyStory(deliveryPaths []map[string]any, sharedConfigPaths []map[string]any) []string {
	story := make([]string, 0, len(deliveryPaths)+1)
	for _, row := range deliveryPaths {
		switch StringVal(row, "kind") {
		case "controller_artifact":
			path := strings.TrimSpace(StringVal(row, "path"))
			controllerKind := strings.TrimSpace(StringVal(row, "controller_kind"))
			if path == "" || controllerKind == "" {
				continue
			}
			details := make([]string, 0, 3)
			if entryPoints := stringSliceValue(row, "entry_points"); len(entryPoints) > 0 {
				details = append(details, "entry points "+strings.Join(entryPoints, ", "))
			}
			if sharedLibraries := stringSliceValue(row, "shared_libraries"); len(sharedLibraries) > 0 {
				details = append(details, "shared libraries "+strings.Join(sharedLibraries, ", "))
			}
			if pipelineCalls := stringSliceValue(row, "pipeline_calls"); len(pipelineCalls) > 0 {
				details = append(details, "pipeline calls "+strings.Join(pipelineCalls, ", "))
			}
			if hints := mapSliceValue(row, "ansible_playbook_hints"); len(hints) > 0 {
				playbooks := make([]string, 0, len(hints))
				for _, hint := range hints {
					if playbook := strings.TrimSpace(StringVal(hint, "playbook")); playbook != "" {
						playbooks = append(playbooks, playbook)
					}
				}
				if len(playbooks) > 0 {
					details = append(details, "ansible playbooks "+strings.Join(playbooks, ", "))
				}
			}
			if inventories := stringSliceValue(row, "ansible_inventories"); len(inventories) > 0 {
				details = append(details, "ansible inventories "+strings.Join(inventories, ", "))
			}
			if varFiles := stringSliceValue(row, "ansible_var_files"); len(varFiles) > 0 {
				details = append(details, "ansible vars "+strings.Join(varFiles, ", "))
			}
			if taskEntrypoints := stringSliceValue(row, "ansible_task_entrypoints"); len(taskEntrypoints) > 0 {
				details = append(details, "ansible task entrypoints "+strings.Join(taskEntrypoints, ", "))
			}
			line := fmt.Sprintf("Controller delivery paths include %s via %s", path, controllerKind)
			if len(details) > 0 {
				line += " (" + strings.Join(details, "; ") + ")"
			}
			story = append(story, line+".")
		case "runtime_artifact":
			path := strings.TrimSpace(StringVal(row, "path"))
			artifactType := strings.TrimSpace(StringVal(row, "artifact_type"))
			artifactName := strings.TrimSpace(StringVal(row, "artifact_name"))
			serviceName := strings.TrimSpace(StringVal(row, "service_name"))
			baseImage := strings.TrimSpace(StringVal(row, "base_image"))
			cmd := strings.TrimSpace(StringVal(row, "cmd"))
			buildContext := strings.TrimSpace(StringVal(row, "build_context"))
			envFiles := stringSliceValue(row, "env_files")
			configs := stringSliceValue(row, "configs")
			secrets := stringSliceValue(row, "secrets")
			signals := stringSliceValue(row, "signals")
			if path == "" || artifactType == "" {
				continue
			}
			line := buildRuntimeArtifactStoryLine(artifactType, artifactName, serviceName, path, baseImage, cmd)
			if line == "" {
				continue
			}
			if buildContext != "" && serviceName != "" {
				line += fmt.Sprintf(" built from %s", buildContext)
			}
			runtimeDetails := make([]string, 0, 3)
			if len(envFiles) > 0 {
				runtimeDetails = append(runtimeDetails, "env files "+strings.Join(envFiles, ", "))
			}
			if len(configs) > 0 {
				runtimeDetails = append(runtimeDetails, "configs "+strings.Join(configs, ", "))
			}
			if len(secrets) > 0 {
				runtimeDetails = append(runtimeDetails, "secrets "+strings.Join(secrets, ", "))
			}
			if len(runtimeDetails) > 0 {
				line += " with " + joinSentenceFragments(runtimeDetails)
			}
			if len(signals) > 0 {
				line += fmt.Sprintf(" (%s)", strings.Join(signals, ", "))
			}
			story = append(story, line+".")
		case "config_artifact":
			path := strings.TrimSpace(StringVal(row, "path"))
			sourceRepo := strings.TrimSpace(StringVal(row, "source_repo"))
			relativePath := strings.TrimSpace(StringVal(row, "relative_path"))
			evidenceKind := strings.TrimSpace(StringVal(row, "evidence_kind"))
			if path == "" || sourceRepo == "" || relativePath == "" || evidenceKind == "" {
				continue
			}
			story = append(story, fmt.Sprintf(
				"Config provenance includes %s from %s via %s in %s.",
				path,
				sourceRepo,
				evidenceKind,
				relativePath,
			))
		case "workflow_artifact":
			path := strings.TrimSpace(StringVal(row, "path"))
			artifactType := strings.TrimSpace(StringVal(row, "artifact_type"))
			workflowName := strings.TrimSpace(StringVal(row, "workflow_name"))
			if path == "" || artifactType == "" {
				continue
			}
			line := fmt.Sprintf("Workflow delivery paths include %s", path)
			if workflowName != "" {
				line += fmt.Sprintf(" as %s %s", artifactType, workflowName)
			}
			if triggerEvents := stringSliceValue(row, "trigger_events"); len(triggerEvents) > 0 {
				line += fmt.Sprintf(" triggered by %s", strings.Join(triggerEvents, ", "))
			}
			if workflowInputs := stringSliceValue(row, "workflow_inputs"); len(workflowInputs) > 0 {
				line += fmt.Sprintf(" with workflow inputs %s", strings.Join(workflowInputs, ", "))
			}
			hasGovernance := false
			if permissionScopes := stringSliceValue(row, "permission_scopes"); len(permissionScopes) > 0 {
				line += fmt.Sprintf(" with permissions %s", strings.Join(permissionScopes, ", "))
				hasGovernance = true
			}
			if concurrencyGroups := stringSliceValue(row, "concurrency_groups"); len(concurrencyGroups) > 0 {
				if hasGovernance {
					line += fmt.Sprintf(", concurrency %s", strings.Join(concurrencyGroups, ", "))
				} else {
					line += fmt.Sprintf(" with concurrency %s", strings.Join(concurrencyGroups, ", "))
					hasGovernance = true
				}
			}
			if environments := stringSliceValue(row, "environments"); len(environments) > 0 {
				if hasGovernance {
					line += fmt.Sprintf(", environments %s", strings.Join(environments, ", "))
				} else {
					line += fmt.Sprintf(" with environments %s", strings.Join(environments, ", "))
					hasGovernance = true
				}
			}
			if jobTimeouts := stringSliceValue(row, "job_timeout_minutes"); len(jobTimeouts) > 0 {
				if hasGovernance {
					line += fmt.Sprintf(", and job timeouts %s", strings.Join(jobTimeouts, ", "))
				} else {
					line += fmt.Sprintf(" with job timeouts %s", strings.Join(jobTimeouts, ", "))
					hasGovernance = true
				}
			}
			if matrixKeys := stringSliceValue(row, "matrix_keys"); len(matrixKeys) > 0 {
				line += fmt.Sprintf(" and matrix %s", strings.Join(matrixKeys, ", "))
				if matrixCombinationCount := intValue(row, "matrix_combination_count"); matrixCombinationCount > 0 {
					line += fmt.Sprintf(" (%d combination(s))", matrixCombinationCount)
				}
			}
			if commandCount := intValue(row, "command_count"); commandCount > 0 {
				line += fmt.Sprintf(" with %d run command(s)", commandCount)
			}
			if deliveryLocalPaths := stringSliceValue(row, "delivery_local_paths"); len(deliveryLocalPaths) > 0 {
				line += fmt.Sprintf(" using local paths %s", strings.Join(deliveryLocalPaths, ", "))
			}
			if deliveryFamilies := stringSliceValue(row, "delivery_command_families"); len(deliveryFamilies) > 0 {
				line += fmt.Sprintf(" and delivery families %s", strings.Join(deliveryFamilies, ", "))
			}
			if gatingConditions := stringSliceValue(row, "gating_conditions"); len(gatingConditions) > 0 {
				line += fmt.Sprintf(", gating conditions %s", strings.Join(gatingConditions, "; "))
			}
			if needsDependencies := stringSliceValue(row, "needs_dependencies"); len(needsDependencies) > 0 {
				line += fmt.Sprintf(", and needs %s", strings.Join(needsDependencies, ", "))
			}
			if localWorkflowPaths := stringSliceValue(row, "local_reusable_workflow_paths"); len(localWorkflowPaths) > 0 {
				line += fmt.Sprintf(" via local reusable workflow paths %s", strings.Join(localWorkflowPaths, ", "))
			}
			if reusableWorkflows := stringSliceValue(row, "reusable_workflow_repositories"); len(reusableWorkflows) > 0 {
				if strings.Contains(line, " via local reusable workflow paths ") {
					line += fmt.Sprintf(" and reusable workflow repos %s", strings.Join(reusableWorkflows, ", "))
				} else {
					line += fmt.Sprintf(" via reusable workflow repos %s", strings.Join(reusableWorkflows, ", "))
				}
			}
			if checkoutRepos := stringSliceValue(row, "checkout_repositories"); len(checkoutRepos) > 0 {
				if strings.Contains(line, " via reusable workflow repos ") {
					line += fmt.Sprintf(" and checkout repos %s", strings.Join(checkoutRepos, ", "))
				} else {
					line += fmt.Sprintf(" via checkout repos %s", strings.Join(checkoutRepos, ", "))
				}
			}
			if actionRepos := stringSliceValue(row, "action_repositories"); len(actionRepos) > 0 {
				switch {
				case strings.Contains(line, " via reusable workflow repos "),
					strings.Contains(line, " via checkout repos "),
					strings.Contains(line, " and checkout repos "):
					line += fmt.Sprintf(" and action repos %s", strings.Join(actionRepos, ", "))
				default:
					line += fmt.Sprintf(" via action repos %s", strings.Join(actionRepos, ", "))
				}
			}
			if workflowInputRepos := stringSliceValue(row, "workflow_input_repositories"); len(workflowInputRepos) > 0 {
				if strings.Contains(line, " via reusable workflow repos ") ||
					strings.Contains(line, " via checkout repos ") ||
					strings.Contains(line, " and checkout repos ") ||
					strings.Contains(line, " via action repos ") ||
					strings.Contains(line, " and action repos ") {
					line += fmt.Sprintf(" and workflow input repos %s", strings.Join(workflowInputRepos, ", "))
				} else {
					line += fmt.Sprintf(" via workflow input repos %s", strings.Join(workflowInputRepos, ", "))
				}
			}
			if signals := stringSliceValue(row, "signals"); len(signals) > 0 {
				line += fmt.Sprintf(" (%s)", strings.Join(signals, ", "))
			}
			story = append(story, line+".")
		}
	}

	if len(sharedConfigPaths) > 0 {
		families := make([]string, 0, len(sharedConfigPaths))
		for _, row := range sharedConfigPaths {
			path := strings.TrimSpace(StringVal(row, "path"))
			sourceRepos := stringSliceValue(row, "source_repositories")
			if path == "" || len(sourceRepos) == 0 {
				continue
			}
			families = append(families, fmt.Sprintf("%s across %s", path, strings.Join(sourceRepos, ", ")))
		}
		if len(families) > 0 {
			story = append(story, "Shared config families span "+joinSentenceFragments(families)+".")
		}
	}

	return story
}

func buildOverviewDeliveryFamilyStory(deliveryFamilyPaths []map[string]any) []string {
	if len(deliveryFamilyPaths) == 0 {
		return nil
	}

	story := make([]string, 0, len(deliveryFamilyPaths))
	for _, row := range deliveryFamilyPaths {
		switch StringVal(row, "family") {
		case "cloudformation":
			path := strings.TrimSpace(StringVal(row, "path"))
			artifactType := strings.TrimSpace(StringVal(row, "artifact_type"))
			if path == "" || artifactType == "" {
				continue
			}
			story = append(story, fmt.Sprintf(
				"CloudFormation serverless delivery is evidenced by %s via %s.",
				path,
				artifactType,
			))
		case "docker_compose":
			path := strings.TrimSpace(StringVal(row, "path"))
			serviceName := strings.TrimSpace(StringVal(row, "service_name"))
			if path == "" {
				continue
			}
			line := fmt.Sprintf(
				"Docker Compose runtime evidence is present via %s",
				path,
			)
			if serviceName != "" {
				line += fmt.Sprintf(" for service %s", serviceName)
			}
			line += "; treat it as development/runtime evidence unless stronger production deployment proof exists."
			story = append(story, line)
		case "gitops":
			relType := strings.TrimSpace(StringVal(row, "type"))
			targetName := strings.TrimSpace(StringVal(row, "target_name"))
			evidenceType := strings.TrimSpace(StringVal(row, "evidence_type"))
			if relType == "" || targetName == "" || evidenceType == "" {
				continue
			}
			story = append(story, fmt.Sprintf(
				"GitOps delivery is evidenced by %s %s via %s.",
				relType,
				targetName,
				evidenceType,
			))
		case "jenkins":
			path := strings.TrimSpace(StringVal(row, "path"))
			controllerKind := strings.TrimSpace(StringVal(row, "controller_kind"))
			if path == "" || controllerKind == "" {
				continue
			}
			story = append(story, fmt.Sprintf(
				"Jenkins delivery is evidenced by %s via %s.",
				path,
				controllerKind,
			))
		}
	}

	return story
}

func buildRuntimeArtifactStoryLine(artifactType, artifactName, serviceName, path, baseImage, cmd string) string {
	switch {
	case serviceName != "":
		line := fmt.Sprintf("Runtime artifacts include %s service %s in %s", artifactType, serviceName, path)
		if cmd != "" {
			line += fmt.Sprintf(" with cmd %s", cmd)
		}
		return line
	case artifactName != "":
		line := fmt.Sprintf("Runtime artifacts include %s stage %s in %s", artifactType, artifactName, path)
		if baseImage != "" {
			line += fmt.Sprintf(" based on %s", baseImage)
		}
		if cmd != "" {
			line += fmt.Sprintf(" with cmd %s", cmd)
		}
		return line
	default:
		return ""
	}
}
