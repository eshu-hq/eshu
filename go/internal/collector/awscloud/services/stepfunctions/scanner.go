// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package stepfunctions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Step Functions metadata facts for one claimed account and
// region. It never starts, stops, sends, creates, updates, or deletes Step
// Functions resources, and it never persists execution input, execution
// output, execution history events, task tokens, or literal
// Parameters/ResultPath/ResultSelector contents from a state machine
// definition.
type Scanner struct {
	Client Client
}

// Scan observes Step Functions state machines and activities through the
// configured client and returns reported-confidence AWS facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("stepfunctions scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceStepFunctions:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceStepFunctions
	default:
		return nil, fmt.Errorf("stepfunctions scanner received service_kind %q", boundary.ServiceKind)
	}

	stateMachines, err := s.Client.ListStateMachines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Step Functions state machines: %w", err)
	}
	var envelopes []facts.Envelope
	for _, machine := range stateMachines {
		resource, err := awscloud.NewResourceEnvelope(stateMachineObservation(boundary, machine))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if role, ok := stateMachineRoleRelationship(boundary, machine); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(role)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
		for _, reference := range uniqueARNs(machine.ReferencedARNs) {
			envelope, err := awscloud.NewRelationshipEnvelope(
				stateMachineReferenceRelationship(boundary, machine, reference),
			)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	activities, err := s.Client.ListActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Step Functions activities: %w", err)
	}
	for _, activity := range activities {
		envelope, err := awscloud.NewResourceEnvelope(activityObservation(boundary, activity))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func stateMachineObservation(
	boundary awscloud.Boundary,
	machine StateMachine,
) awscloud.ResourceObservation {
	machineARN := strings.TrimSpace(machine.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          machineARN,
		ResourceID:   firstNonEmpty(machineARN, machine.Name),
		ResourceType: awscloud.ResourceTypeStepFunctionsStateMachine,
		Name:         strings.TrimSpace(machine.Name),
		State:        strings.TrimSpace(machine.Status),
		Tags:         cloneStringMap(machine.Tags),
		Attributes: map[string]any{
			"type":            strings.TrimSpace(machine.Type),
			"role_arn":        strings.TrimSpace(machine.RoleARN),
			"creation_date":   timeOrNil(machine.CreationDate),
			"logging_level":   strings.TrimSpace(machine.LoggingLevel),
			"tracing_enabled": machine.TracingEnabled,
			"start_at":        strings.TrimSpace(machine.StartAt),
			"states":          safeStateNodes(machine.States),
		},
		CorrelationAnchors: []string{machineARN, machine.Name},
		SourceRecordID:     firstNonEmpty(machineARN, machine.Name),
	}
}

func activityObservation(boundary awscloud.Boundary, activity Activity) awscloud.ResourceObservation {
	activityARN := strings.TrimSpace(activity.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          activityARN,
		ResourceID:   firstNonEmpty(activityARN, activity.Name),
		ResourceType: awscloud.ResourceTypeStepFunctionsActivity,
		Name:         strings.TrimSpace(activity.Name),
		Tags:         cloneStringMap(activity.Tags),
		Attributes: map[string]any{
			"creation_date": timeOrNil(activity.CreationDate),
		},
		CorrelationAnchors: []string{activityARN, activity.Name},
		SourceRecordID:     firstNonEmpty(activityARN, activity.Name),
	}
}

func stateMachineRoleRelationship(
	boundary awscloud.Boundary,
	machine StateMachine,
) (awscloud.RelationshipObservation, bool) {
	machineARN := strings.TrimSpace(machine.ARN)
	roleARN := strings.TrimSpace(machine.RoleARN)
	if machineARN == "" || !isARN(roleARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStepFunctionsStateMachineUsesIAMRole,
		SourceResourceID: machineARN,
		SourceARN:        machineARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   machineARN + "#execution-role#" + roleARN,
	}, true
}

func stateMachineReferenceRelationship(
	boundary awscloud.Boundary,
	machine StateMachine,
	targetARN string,
) awscloud.RelationshipObservation {
	machineARN := strings.TrimSpace(machine.ARN)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipStepFunctionsStateMachineReferencesResource,
		SourceResourceID: firstNonEmpty(machineARN, machine.Name),
		SourceARN:        machineARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetTypeForARN(targetARN),
		SourceRecordID:   firstNonEmpty(machineARN, machine.Name) + "->" + targetARN,
	}
}

// safeStateNodes returns the persisted projection of a state machine's state
// list. It includes only structural fields (name, type, end, next, default,
// choice and catch transitions, Task resource ARN). Parameters, ResultPath,
// ResultSelector, InputPath, OutputPath, Result, and similar literal payload
// contents are intentionally excluded; the security gate for this scanner
// requires those fields to stay off the persisted attribute.
//
// The resource_arn field is gated on isARN so service-integration identifiers
// (e.g. "states:::lambda:invoke.waitForTaskToken") that AWS reports in the
// Resource field of a Task state are not persisted as if they were ARNs. The
// scanner-level reference relationship is gated on the same check.
func safeStateNodes(states []StateNode) []map[string]any {
	if len(states) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(states))
	for _, state := range states {
		node := map[string]any{
			"name": strings.TrimSpace(state.Name),
			"type": strings.TrimSpace(state.Type),
			"end":  state.End,
		}
		if next := strings.TrimSpace(state.Next); next != "" {
			node["next"] = next
		}
		if def := strings.TrimSpace(state.Default); def != "" {
			node["default"] = def
		}
		if choices := dedupeStrings(state.Choices); len(choices) > 0 {
			node["choice_next"] = choices
		}
		if catches := dedupeStrings(state.CatchNext); len(catches) > 0 {
			node["catch_next"] = catches
		}
		if resourceARN := strings.TrimSpace(state.ResourceARN); isARN(resourceARN) {
			node["resource_arn"] = resourceARN
		}
		out = append(out, node)
	}
	return out
}

func targetTypeForARN(arn string) string {
	switch {
	case strings.Contains(arn, ":lambda:"):
		return awscloud.ResourceTypeLambdaFunction
	case strings.Contains(arn, ":sns:"):
		return awscloud.ResourceTypeSNSTopic
	case strings.Contains(arn, ":sqs:"):
		return awscloud.ResourceTypeSQSQueue
	case strings.Contains(arn, ":states:"):
		return awscloud.ResourceTypeStepFunctionsStateMachine
	case strings.Contains(arn, ":dynamodb:"):
		return awscloud.ResourceTypeDynamoDBTable
	case strings.Contains(arn, ":ecs:"):
		return awscloud.ResourceTypeECSTaskDefinition
	default:
		return "aws_resource"
	}
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func uniqueARNs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if !isARN(trimmed) {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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
