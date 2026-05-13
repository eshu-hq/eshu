package elbv2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS ELBv2 load balancer, listener, target group, rule, and
// relationship facts for one claimed account and region.
type Scanner struct {
	Client Client
}

// Scan observes ELBv2 routing topology through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("elbv2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceELBv2
	case awscloud.ServiceELBv2:
	default:
		return nil, fmt.Errorf("elbv2 scanner received service_kind %q", boundary.ServiceKind)
	}

	loadBalancers, err := s.Client.ListLoadBalancers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ELBv2 load balancers: %w", err)
	}
	var envelopes []facts.Envelope
	for _, loadBalancer := range loadBalancers {
		loadBalancerEnvelopes, err := s.loadBalancerEnvelopes(ctx, boundary, loadBalancer)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, loadBalancerEnvelopes...)
	}

	targetGroups, err := s.Client.ListTargetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ELBv2 target groups: %w", err)
	}
	for _, targetGroup := range targetGroups {
		targetGroupEnvelopes, err := targetGroupEnvelopes(boundary, targetGroup)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, targetGroupEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) loadBalancerEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	loadBalancer LoadBalancer,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(loadBalancerObservation(boundary, loadBalancer))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	listeners, err := s.Client.ListListeners(ctx, loadBalancer)
	if err != nil {
		return nil, fmt.Errorf("list ELBv2 listeners for load balancer %q: %w", loadBalancer.Name, err)
	}
	for _, listener := range listeners {
		listenerEnvelopes, err := s.listenerEnvelopes(ctx, boundary, listener)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, listenerEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) listenerEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	listener Listener,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(listenerObservation(boundary, listener))
	if err != nil {
		return nil, err
	}
	relationship, err := awscloud.NewRelationshipEnvelope(loadBalancerListenerRelationship(boundary, listener))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource, relationship}
	routeBuilder := newRouteRelationshipBuilder(boundary, listener)
	routeBuilder.addActions("default_action", "", "", listener.DefaultActions)

	rules, err := s.Client.ListRules(ctx, listener)
	if err != nil {
		return nil, fmt.Errorf("list ELBv2 rules for listener %q: %w", listener.ARN, err)
	}
	for _, rule := range rules {
		ruleResource, err := awscloud.NewResourceEnvelope(ruleObservation(boundary, rule))
		if err != nil {
			return nil, err
		}
		ruleRelationship, err := awscloud.NewRelationshipEnvelope(listenerRuleRelationship(boundary, rule))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, ruleResource, ruleRelationship)
		routeBuilder.addActions("rule_action", rule.ARN, rule.Priority, rule.Actions)
	}
	for _, observation := range routeBuilder.observations() {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func loadBalancerObservation(boundary awscloud.Boundary, loadBalancer LoadBalancer) awscloud.ResourceObservation {
	loadBalancerARN := strings.TrimSpace(loadBalancer.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          loadBalancerARN,
		ResourceID:   loadBalancerARN,
		ResourceType: awscloud.ResourceTypeELBv2LoadBalancer,
		Name:         loadBalancer.Name,
		State:        loadBalancer.State,
		Tags:         loadBalancer.Tags,
		Attributes: map[string]any{
			"availability_zones":       availabilityZoneMaps(loadBalancer.AvailabilityZones),
			"canonical_hosted_zone_id": strings.TrimSpace(loadBalancer.CanonicalHostedZoneID),
			"created_at":               timeOrNil(loadBalancer.CreatedAt),
			"dns_name":                 strings.TrimSpace(loadBalancer.DNSName),
			"ip_address_type":          strings.TrimSpace(loadBalancer.IPAddressType),
			"scheme":                   strings.TrimSpace(loadBalancer.Scheme),
			"security_groups":          cloneStrings(loadBalancer.SecurityGroups),
			"type":                     strings.TrimSpace(loadBalancer.Type),
			"vpc_id":                   strings.TrimSpace(loadBalancer.VPCID),
		},
		CorrelationAnchors: []string{loadBalancerARN, loadBalancer.Name, loadBalancer.DNSName},
		SourceRecordID:     loadBalancerARN,
	}
}

func listenerObservation(boundary awscloud.Boundary, listener Listener) awscloud.ResourceObservation {
	listenerARN := strings.TrimSpace(listener.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          listenerARN,
		ResourceID:   listenerARN,
		ResourceType: awscloud.ResourceTypeELBv2Listener,
		Name:         listenerARN,
		Tags:         listener.Tags,
		Attributes: map[string]any{
			"alpn_policy":       cloneStrings(listener.ALPNPolicy),
			"certificates":      cloneStrings(listener.Certificates),
			"default_actions":   actionMaps(listener.DefaultActions),
			"load_balancer_arn": strings.TrimSpace(listener.LoadBalancerARN),
			"port":              listener.Port,
			"protocol":          strings.TrimSpace(listener.Protocol),
			"ssl_policy":        strings.TrimSpace(listener.SSLPolicy),
		},
		CorrelationAnchors: []string{listenerARN, listener.LoadBalancerARN},
		SourceRecordID:     listenerARN,
	}
}

func ruleObservation(boundary awscloud.Boundary, rule Rule) awscloud.ResourceObservation {
	ruleARN := strings.TrimSpace(rule.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          ruleARN,
		ResourceID:   ruleARN,
		ResourceType: awscloud.ResourceTypeELBv2Rule,
		Name:         ruleARN,
		Tags:         rule.Tags,
		Attributes: map[string]any{
			"actions":      actionMaps(rule.Actions),
			"conditions":   conditionMaps(rule.Conditions),
			"is_default":   rule.IsDefault,
			"listener_arn": strings.TrimSpace(rule.ListenerARN),
			"priority":     strings.TrimSpace(rule.Priority),
		},
		CorrelationAnchors: []string{ruleARN, rule.ListenerARN},
		SourceRecordID:     ruleARN,
	}
}

func targetGroupObservation(boundary awscloud.Boundary, targetGroup TargetGroup) awscloud.ResourceObservation {
	targetGroupARN := strings.TrimSpace(targetGroup.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          targetGroupARN,
		ResourceID:   targetGroupARN,
		ResourceType: awscloud.ResourceTypeELBv2TargetGroup,
		Name:         targetGroup.Name,
		Tags:         targetGroup.Tags,
		Attributes: map[string]any{
			"health_check":       healthCheckMap(targetGroup.HealthCheck),
			"ip_address_type":    strings.TrimSpace(targetGroup.IPAddressType),
			"load_balancer_arns": cloneStrings(targetGroup.LoadBalancerARNs),
			"port":               targetGroup.Port,
			"protocol":           strings.TrimSpace(targetGroup.Protocol),
			"protocol_version":   strings.TrimSpace(targetGroup.ProtocolVersion),
			"target_type":        strings.TrimSpace(targetGroup.TargetType),
			"vpc_id":             strings.TrimSpace(targetGroup.VPCID),
		},
		CorrelationAnchors: []string{targetGroupARN, targetGroup.Name},
		SourceRecordID:     targetGroupARN,
	}
}

func targetGroupEnvelopes(boundary awscloud.Boundary, targetGroup TargetGroup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(targetGroupObservation(boundary, targetGroup))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, loadBalancerARN := range targetGroup.LoadBalancerARNs {
		relationship, err := awscloud.NewRelationshipEnvelope(
			targetGroupLoadBalancerRelationship(boundary, targetGroup, loadBalancerARN),
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func loadBalancerListenerRelationship(
	boundary awscloud.Boundary,
	listener Listener,
) awscloud.RelationshipObservation {
	listenerARN := strings.TrimSpace(listener.ARN)
	loadBalancerARN := strings.TrimSpace(listener.LoadBalancerARN)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipELBv2LoadBalancerHasListener,
		SourceResourceID: loadBalancerARN,
		SourceARN:        loadBalancerARN,
		TargetResourceID: listenerARN,
		TargetARN:        listenerARN,
		TargetType:       awscloud.ResourceTypeELBv2Listener,
		SourceRecordID:   loadBalancerARN + "#listener#" + listenerARN,
	}
}

func listenerRuleRelationship(boundary awscloud.Boundary, rule Rule) awscloud.RelationshipObservation {
	listenerARN := strings.TrimSpace(rule.ListenerARN)
	ruleARN := strings.TrimSpace(rule.ARN)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipELBv2ListenerHasRule,
		SourceResourceID: listenerARN,
		SourceARN:        listenerARN,
		TargetResourceID: ruleARN,
		TargetARN:        ruleARN,
		TargetType:       awscloud.ResourceTypeELBv2Rule,
		Attributes: map[string]any{
			"is_default": rule.IsDefault,
			"priority":   strings.TrimSpace(rule.Priority),
		},
		SourceRecordID: listenerARN + "#rule#" + ruleARN,
	}
}

func targetGroupLoadBalancerRelationship(
	boundary awscloud.Boundary,
	targetGroup TargetGroup,
	loadBalancerARN string,
) awscloud.RelationshipObservation {
	targetGroupARN := strings.TrimSpace(targetGroup.ARN)
	loadBalancerARN = strings.TrimSpace(loadBalancerARN)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipELBv2TargetGroupAttachedToLoadBalancer,
		SourceResourceID: targetGroupARN,
		SourceARN:        targetGroupARN,
		TargetResourceID: loadBalancerARN,
		TargetARN:        loadBalancerARN,
		TargetType:       awscloud.ResourceTypeELBv2LoadBalancer,
		SourceRecordID:   targetGroupARN + "#load-balancer#" + loadBalancerARN,
	}
}

func availabilityZoneMaps(zones []AvailabilityZone) []map[string]string {
	if len(zones) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(zones))
	for _, zone := range zones {
		output = append(output, map[string]string{
			"name":      strings.TrimSpace(zone.Name),
			"subnet_id": strings.TrimSpace(zone.SubnetID),
		})
	}
	return output
}

func healthCheckMap(check HealthCheck) map[string]any {
	return map[string]any{
		"enabled":             check.Enabled,
		"healthy_threshold":   check.HealthyThreshold,
		"interval_seconds":    check.IntervalSeconds,
		"matcher":             strings.TrimSpace(check.Matcher),
		"path":                strings.TrimSpace(check.Path),
		"port":                strings.TrimSpace(check.Port),
		"protocol":            strings.TrimSpace(check.Protocol),
		"timeout_seconds":     check.TimeoutSeconds,
		"unhealthy_threshold": check.UnhealthyThreshold,
	}
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
