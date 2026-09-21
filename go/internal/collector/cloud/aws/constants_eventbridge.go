// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceEventBridge identifies the regional Amazon EventBridge metadata
	// scan slice.
	ServiceEventBridge = "eventbridge"
)

const (
	// ResourceTypeEventBridgeEventBus identifies an EventBridge event bus
	// metadata resource.
	ResourceTypeEventBridgeEventBus = "aws_eventbridge_event_bus"
	// ResourceTypeEventBridgeRule identifies an EventBridge rule metadata
	// resource.
	ResourceTypeEventBridgeRule = "aws_eventbridge_rule"
)

const (
	// RelationshipEventBridgeRuleOnEventBus records EventBridge rule
	// membership on an event bus.
	RelationshipEventBridgeRuleOnEventBus = "eventbridge_rule_on_event_bus"
	// RelationshipEventBridgeRuleTargetsResource records EventBridge rule target
	// evidence when the target is ARN-addressable.
	RelationshipEventBridgeRuleTargetsResource = "eventbridge_rule_targets_resource"
)
