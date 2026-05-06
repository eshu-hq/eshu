package query

import (
	"sort"
	"strings"
)

// buildRepositoryCloudFormationRuntimeArtifacts promotes parser-proven
// CloudFormation/SAM resources into deployment artifact rows without inventing
// relationships the reducer has not materialized.
func buildRepositoryCloudFormationRuntimeArtifacts(infrastructure []map[string]any) map[string]any {
	if len(infrastructure) == 0 {
		return nil
	}

	artifacts := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, row := range infrastructure {
		if strings.TrimSpace(StringVal(row, "type")) != "CloudFormationResource" {
			continue
		}
		resourceType := strings.TrimSpace(StringVal(row, "resource_type"))
		if !isCloudFormationServerlessResource(resourceType) {
			continue
		}
		path := strings.TrimSpace(StringVal(row, "file_path"))
		name := strings.TrimSpace(StringVal(row, "name"))
		if path == "" || name == "" {
			continue
		}
		key := path + "\x00" + name + "\x00" + resourceType
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		artifacts = append(artifacts, map[string]any{
			"relative_path": path,
			"artifact_type": "cloudformation_serverless",
			"artifact_name": name,
			"resource_type": resourceType,
			"signals":       cloudFormationServerlessSignals(resourceType),
		})
	}
	if len(artifacts) == 0 {
		return nil
	}

	sort.SliceStable(artifacts, func(i, j int) bool {
		if left, right := StringVal(artifacts[i], "relative_path"), StringVal(artifacts[j], "relative_path"); left != right {
			return left < right
		}
		return StringVal(artifacts[i], "artifact_name") < StringVal(artifacts[j], "artifact_name")
	})
	return map[string]any{
		"deployment_artifacts": artifacts,
	}
}

func isCloudFormationServerlessResource(resourceType string) bool {
	normalized := strings.TrimSpace(resourceType)
	if strings.HasPrefix(normalized, "AWS::Serverless::") {
		return true
	}
	switch normalized {
	case "AWS::Lambda::Function", "AWS::Lambda::LayerVersion", "AWS::StepFunctions::StateMachine":
		return true
	default:
		return false
	}
}

func cloudFormationServerlessSignals(resourceType string) []string {
	signals := []string{"template_file"}
	normalized := strings.TrimSpace(resourceType)
	switch {
	case strings.HasPrefix(normalized, "AWS::Serverless::"):
		signals = append(signals, "serverless_transform")
	case strings.HasPrefix(normalized, "AWS::Lambda::"):
		signals = append(signals, "lambda_function")
	case normalized == "AWS::StepFunctions::StateMachine":
		signals = append(signals, "state_machine")
	}
	return signals
}
