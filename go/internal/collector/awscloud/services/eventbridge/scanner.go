package eventbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS EventBridge metadata facts for one claimed account and
// region. It never puts events, mutates buses/rules/targets, or persists target
// payload fields.
type Scanner struct {
	Client Client
}

// Scan observes EventBridge event buses, rules, and ARN-shaped targets through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("eventbridge scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceEventBridge
	case awscloud.ServiceEventBridge:
	default:
		return nil, fmt.Errorf("eventbridge scanner received service_kind %q", boundary.ServiceKind)
	}

	buses, err := s.Client.ListEventBuses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EventBridge event buses: %w", err)
	}
	var envelopes []facts.Envelope
	for _, bus := range buses {
		resource, err := awscloud.NewResourceEnvelope(eventBusObservation(boundary, bus))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, rule := range bus.Rules {
			ruleResource, err := awscloud.NewResourceEnvelope(ruleObservation(boundary, rule))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, ruleResource)
			ruleBus, ok := ruleBusRelationship(boundary, bus, rule)
			if ok {
				relationship, err := awscloud.NewRelationshipEnvelope(ruleBus)
				if err != nil {
					return nil, err
				}
				envelopes = append(envelopes, relationship)
			}
			for _, target := range rule.Targets {
				targetRelationship, ok := ruleTargetRelationship(boundary, rule, target)
				if !ok {
					continue
				}
				relationship, err := awscloud.NewRelationshipEnvelope(targetRelationship)
				if err != nil {
					return nil, err
				}
				envelopes = append(envelopes, relationship)
			}
		}
	}
	return envelopes, nil
}

func eventBusObservation(boundary awscloud.Boundary, bus EventBus) awscloud.ResourceObservation {
	busARN := strings.TrimSpace(bus.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          busARN,
		ResourceID:   firstNonEmpty(busARN, bus.Name),
		ResourceType: awscloud.ResourceTypeEventBridgeEventBus,
		Name:         strings.TrimSpace(bus.Name),
		Tags:         cloneStringMap(bus.Tags),
		Attributes: map[string]any{
			"description":        strings.TrimSpace(bus.Description),
			"creation_time":      timeOrNil(bus.CreationTime),
			"last_modified_time": timeOrNil(bus.LastModifiedTime),
		},
		CorrelationAnchors: []string{busARN, bus.Name},
		SourceRecordID:     firstNonEmpty(busARN, bus.Name),
	}
}

func ruleObservation(boundary awscloud.Boundary, rule Rule) awscloud.ResourceObservation {
	ruleARN := strings.TrimSpace(rule.ARN)
	resourceID := firstNonEmpty(ruleARN, joinNonEmpty("/", rule.EventBusName, rule.Name), rule.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          ruleARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeEventBridgeRule,
		Name:         strings.TrimSpace(rule.Name),
		State:        strings.TrimSpace(rule.State),
		Tags:         cloneStringMap(rule.Tags),
		Attributes: map[string]any{
			"created_by":          strings.TrimSpace(rule.CreatedBy),
			"description":         strings.TrimSpace(rule.Description),
			"event_bus_name":      strings.TrimSpace(rule.EventBusName),
			"event_pattern":       strings.TrimSpace(rule.EventPattern),
			"managed_by":          strings.TrimSpace(rule.ManagedBy),
			"role_arn":            strings.TrimSpace(rule.RoleARN),
			"schedule_expression": strings.TrimSpace(rule.ScheduleExpression),
			"state":               strings.TrimSpace(rule.State),
		},
		CorrelationAnchors: []string{ruleARN, rule.Name, joinNonEmpty("/", rule.EventBusName, rule.Name)},
		SourceRecordID:     resourceID,
	}
}

func ruleBusRelationship(
	boundary awscloud.Boundary,
	bus EventBus,
	rule Rule,
) (awscloud.RelationshipObservation, bool) {
	ruleID := firstNonEmpty(rule.ARN, joinNonEmpty("/", rule.EventBusName, rule.Name), rule.Name)
	busID := firstNonEmpty(bus.ARN, bus.Name, rule.EventBusName)
	if ruleID == "" || busID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEventBridgeRuleOnEventBus,
		SourceResourceID: ruleID,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: busID,
		TargetARN:        strings.TrimSpace(bus.ARN),
		TargetType:       awscloud.ResourceTypeEventBridgeEventBus,
		Attributes: map[string]any{
			"event_bus_name": strings.TrimSpace(firstNonEmpty(rule.EventBusName, bus.Name)),
			"rule_name":      strings.TrimSpace(rule.Name),
		},
		SourceRecordID: ruleID + "->" + busID,
	}, true
}

func ruleTargetRelationship(
	boundary awscloud.Boundary,
	rule Rule,
	target Target,
) (awscloud.RelationshipObservation, bool) {
	ruleID := firstNonEmpty(rule.ARN, joinNonEmpty("/", rule.EventBusName, rule.Name), rule.Name)
	targetARN := strings.TrimSpace(target.ARN)
	if ruleID == "" || !isARN(targetARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEventBridgeRuleTargetsResource,
		SourceResourceID: ruleID,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetTypeForARN(targetARN),
		Attributes: map[string]any{
			"dead_letter_arn":              strings.TrimSpace(target.DeadLetterARN),
			"maximum_event_age_in_seconds": target.MaximumEventAgeInSeconds,
			"maximum_retry_attempts":       target.MaximumRetryAttempts,
			"role_arn":                     strings.TrimSpace(target.RoleARN),
			"target_id":                    strings.TrimSpace(target.ID),
		},
		SourceRecordID: firstNonEmpty(target.ID, ruleID+"->"+targetARN),
	}, true
}

func targetTypeForARN(arn string) string {
	switch {
	case strings.Contains(arn, ":lambda:"):
		return awscloud.ResourceTypeLambdaFunction
	case strings.Contains(arn, ":sqs:"):
		return awscloud.ResourceTypeSQSQueue
	case strings.Contains(arn, ":sns:"):
		return awscloud.ResourceTypeSNSTopic
	case strings.Contains(arn, ":ecs:"):
		return awscloud.ResourceTypeECSCluster
	default:
		return "aws_resource"
	}
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func joinNonEmpty(separator string, values ...string) string {
	var parts []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, separator)
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
