// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceRoute53RecoveryControlConfig identifies the Amazon Route 53
	// Application Recovery Controller recovery-control configuration scan slice.
	// The scanner reads the control-plane List/Describe APIs of the
	// route53recoverycontrolconfig service (ListClusters, ListControlPanels,
	// ListRoutingControls, ListSafetyRules, ListTagsForResource) and never
	// changes a routing control state, never reads routing control state through
	// the separate route53recoverycluster data-plane endpoint, and never mutates
	// any recovery-control configuration resource. This is distinct from
	// ServiceRoute53 (hosted zones / DNS) and ServiceRoute53Resolver.
	ServiceRoute53RecoveryControlConfig = "route53recoverycontrolconfig"
)

const (
	// ResourceTypeRoute53RecoveryControlConfigCluster identifies a Route 53
	// Application Recovery Controller cluster: the highly available data plane
	// that hosts routing control states. The scanner emits identity (cluster
	// ARN, name), deployment status, network type, owner account, and the set of
	// regional cluster endpoint Regions (never used to read or set routing
	// control state).
	ResourceTypeRoute53RecoveryControlConfigCluster = "aws_route53recoverycontrolconfig_cluster"
	// ResourceTypeRoute53RecoveryControlConfigControlPanel identifies a Route 53
	// Application Recovery Controller control panel: a group of routing controls
	// that change together within one cluster. The scanner emits identity
	// (control panel ARN, name), owning cluster ARN, deployment status, the
	// default-control-panel flag, routing control count, and owner account.
	ResourceTypeRoute53RecoveryControlConfigControlPanel = "aws_route53recoverycontrolconfig_control_panel"
	// ResourceTypeRoute53RecoveryControlConfigRoutingControl identifies a Route 53
	// Application Recovery Controller routing control: the on/off switch whose
	// state directs traffic for an application failover. The scanner emits
	// identity (routing control ARN, name), owning control panel ARN, deployment
	// status, and owner account only; it never reads or persists the routing
	// control's live On/Off state, which lives behind the data-plane endpoint.
	ResourceTypeRoute53RecoveryControlConfigRoutingControl = "aws_route53recoverycontrolconfig_routing_control"
	// ResourceTypeRoute53RecoveryControlConfigSafetyRule identifies a Route 53
	// Application Recovery Controller safety rule (an assertion rule or a gating
	// rule) that guards routing control state changes within a control panel. The
	// scanner emits identity (safety rule ARN, name), owning control panel ARN,
	// rule kind (ASSERTION/GATING), deployment status, the rule-config logic
	// (type ATLEAST/AND/OR, threshold, inverted flag), the wait period, and the
	// counts of asserted/gating/target routing controls. It records counts and
	// rule logic only, never application traffic or routing control state.
	ResourceTypeRoute53RecoveryControlConfigSafetyRule = "aws_route53recoverycontrolconfig_safety_rule"
)

const (
	// RelationshipRoute53RecoveryControlConfigControlPanelInCluster records a
	// control panel's membership in its owning cluster. The target is keyed by
	// the cluster ARN the cluster node publishes as its resource_id, so the edge
	// joins the cluster node exactly.
	RelationshipRoute53RecoveryControlConfigControlPanelInCluster = "route53recoverycontrolconfig_control_panel_in_cluster"
	// RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel records
	// a routing control's membership in its owning control panel. The target is
	// keyed by the control panel ARN the control panel node publishes as its
	// resource_id.
	RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel = "route53recoverycontrolconfig_routing_control_in_control_panel"
	// RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel records a
	// safety rule's membership in the control panel it guards. The target is keyed
	// by the control panel ARN the control panel node publishes as its
	// resource_id.
	RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel = "route53recoverycontrolconfig_safety_rule_in_control_panel"
)
