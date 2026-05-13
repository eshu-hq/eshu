package elbv2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type routeRelationshipBuilder struct {
	boundary   awscloud.Boundary
	listener   Listener
	routes     map[string][]map[string]any
	targetARNs []string
}

func newRouteRelationshipBuilder(
	boundary awscloud.Boundary,
	listener Listener,
) *routeRelationshipBuilder {
	return &routeRelationshipBuilder{
		boundary: boundary,
		listener: listener,
		routes:   make(map[string][]map[string]any),
	}
}

func (b *routeRelationshipBuilder) addActions(
	routeSource string,
	ruleARN string,
	rulePriority string,
	actions []Action,
) {
	for _, action := range actions {
		for _, targetGroup := range actionTargetGroups(action) {
			targetGroupARN := strings.TrimSpace(targetGroup.ARN)
			if targetGroupARN == "" {
				continue
			}
			if _, ok := b.routes[targetGroupARN]; !ok {
				b.targetARNs = append(b.targetARNs, targetGroupARN)
			}
			b.routes[targetGroupARN] = append(b.routes[targetGroupARN], map[string]any{
				"action_type":      strings.TrimSpace(action.Type),
				"route_source":     routeSource,
				"rule_arn":         strings.TrimSpace(ruleARN),
				"rule_priority":    strings.TrimSpace(rulePriority),
				"target_weight":    targetGroup.Weight,
				"target_group_arn": targetGroupARN,
			})
		}
	}
}

func (b *routeRelationshipBuilder) observations() []awscloud.RelationshipObservation {
	listenerARN := strings.TrimSpace(b.listener.ARN)
	observations := make([]awscloud.RelationshipObservation, 0, len(b.targetARNs))
	for _, targetGroupARN := range b.targetARNs {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         b.boundary,
			RelationshipType: awscloud.RelationshipELBv2ListenerRoutesToTargetGroup,
			SourceResourceID: listenerARN,
			SourceARN:        listenerARN,
			TargetResourceID: targetGroupARN,
			TargetARN:        targetGroupARN,
			TargetType:       awscloud.ResourceTypeELBv2TargetGroup,
			Attributes: map[string]any{
				"load_balancer_arn": strings.TrimSpace(b.listener.LoadBalancerARN),
				"routes":            b.routes[targetGroupARN],
			},
			SourceRecordID: listenerARN + "#target-group#" + targetGroupARN,
		})
	}
	return observations
}

func actionTargetGroups(action Action) []WeightedTargetGroup {
	var groups []WeightedTargetGroup
	seen := make(map[string]struct{})
	if targetGroupARN := strings.TrimSpace(action.TargetGroupARN); targetGroupARN != "" {
		groups = append(groups, WeightedTargetGroup{ARN: targetGroupARN})
		seen[targetGroupARN] = struct{}{}
	}
	for _, group := range action.ForwardTargetGroups {
		group.ARN = strings.TrimSpace(group.ARN)
		if group.ARN == "" {
			continue
		}
		if _, ok := seen[group.ARN]; ok {
			continue
		}
		groups = append(groups, group)
		seen[group.ARN] = struct{}{}
	}
	return groups
}
