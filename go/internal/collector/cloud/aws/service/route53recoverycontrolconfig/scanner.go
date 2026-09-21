// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53recoverycontrolconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Route 53 Application Recovery Controller recovery-control
// configuration metadata-only facts for one claimed account. It reports
// clusters, control panels, routing controls, and safety rules plus the
// control-panel-in-cluster, routing-control-in-control-panel, and
// safety-rule-in-control-panel relationships. It never reads or sets routing
// control state and never mutates a recovery-control configuration resource.
type Scanner struct {
	// Client is the metadata-only recovery-control configuration snapshot source.
	Client Client
}

// Scan observes recovery-control clusters, their control panels, and the routing
// controls and safety rules under each panel through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("route53recoverycontrolconfig scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceRoute53RecoveryControlConfig:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceRoute53RecoveryControlConfig
	default:
		return nil, fmt.Errorf(
			"route53recoverycontrolconfig scanner received service_kind %q", boundary.ServiceKind,
		)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Route 53 ARC clusters: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, cluster := range snapshot.Clusters {
		next, err := clusterEnvelopes(boundary, cluster)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func clusterEnvelopes(boundary aws.Boundary, cluster Cluster) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, panel := range cluster.ControlPanels {
		next, err := controlPanelEnvelopes(boundary, panel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func controlPanelEnvelopes(boundary aws.Boundary, panel ControlPanel) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(controlPanelObservation(boundary, panel))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationship(&envelopes, controlPanelInClusterRelationship(boundary, panel)); err != nil {
		return nil, err
	}
	for _, control := range panel.RoutingControls {
		resource, err := aws.NewResourceEnvelope(routingControlObservation(boundary, control))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if err := appendRelationship(
			&envelopes, routingControlInControlPanelRelationship(boundary, control),
		); err != nil {
			return nil, err
		}
	}
	for _, rule := range panel.SafetyRules {
		resource, err := aws.NewResourceEnvelope(safetyRuleObservation(boundary, rule))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if err := appendRelationship(
			&envelopes, safetyRuleInControlPanelRelationship(boundary, rule),
		); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendRelationship(envelopes *[]facts.Envelope, relationship *aws.RelationshipObservation) error {
	if relationship == nil {
		return nil
	}
	envelope, err := aws.NewRelationshipEnvelope(*relationship)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func clusterObservation(boundary aws.Boundary, cluster Cluster) aws.ResourceObservation {
	arn := strings.TrimSpace(cluster.ARN)
	name := strings.TrimSpace(cluster.Name)
	resourceID := clusterResourceID(cluster)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRoute53RecoveryControlConfigCluster,
		Name:         name,
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes: map[string]any{
			"cluster_name":     name,
			"network_type":     strings.TrimSpace(cluster.NetworkType),
			"owner":            strings.TrimSpace(cluster.Owner),
			"endpoint_regions": cloneStrings(cluster.EndpointRegions),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func controlPanelObservation(boundary aws.Boundary, panel ControlPanel) aws.ResourceObservation {
	arn := strings.TrimSpace(panel.ARN)
	name := strings.TrimSpace(panel.Name)
	resourceID := controlPanelResourceID(panel)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRoute53RecoveryControlConfigControlPanel,
		Name:         name,
		State:        strings.TrimSpace(panel.Status),
		Tags:         cloneStringMap(panel.Tags),
		Attributes: map[string]any{
			"control_panel_name":    name,
			"cluster_arn":           strings.TrimSpace(panel.ClusterARN),
			"default_control_panel": panel.DefaultControlPanel,
			"routing_control_count": int64(panel.RoutingControlCount),
			"owner":                 strings.TrimSpace(panel.Owner),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func routingControlObservation(boundary aws.Boundary, control RoutingControl) aws.ResourceObservation {
	arn := strings.TrimSpace(control.ARN)
	name := strings.TrimSpace(control.Name)
	resourceID := routingControlResourceID(control)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRoute53RecoveryControlConfigRoutingControl,
		Name:         name,
		State:        strings.TrimSpace(control.Status),
		Tags:         cloneStringMap(control.Tags),
		Attributes: map[string]any{
			"routing_control_name": name,
			"control_panel_arn":    strings.TrimSpace(control.ControlPanelARN),
			"owner":                strings.TrimSpace(control.Owner),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func safetyRuleObservation(boundary aws.Boundary, rule SafetyRule) aws.ResourceObservation {
	arn := strings.TrimSpace(rule.ARN)
	name := strings.TrimSpace(rule.Name)
	resourceID := safetyRuleResourceID(rule)
	attributes := map[string]any{
		"safety_rule_name":      name,
		"control_panel_arn":     strings.TrimSpace(rule.ControlPanelARN),
		"rule_kind":             strings.TrimSpace(rule.RuleKind),
		"wait_period_ms":        int64(rule.WaitPeriodMs),
		"rule_config_type":      strings.TrimSpace(rule.RuleConfigType),
		"rule_config_threshold": int64(rule.RuleConfigThreshold),
		"rule_config_inverted":  rule.RuleConfigInverted,
	}
	switch strings.TrimSpace(rule.RuleKind) {
	case "ASSERTION":
		attributes["asserted_control_count"] = int64(rule.AssertedControlCount)
	case "GATING":
		attributes["gating_control_count"] = int64(rule.GatingControlCount)
		attributes["target_control_count"] = int64(rule.TargetControlCount)
	}
	return aws.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       aws.ResourceTypeRoute53RecoveryControlConfigSafetyRule,
		Name:               name,
		State:              strings.TrimSpace(rule.Status),
		Tags:               cloneStringMap(rule.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
