// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceControlTower identifies the regional AWS Control Tower metadata-only
	// scan slice. The scanner reads landing-zone, enabled-control, and
	// enabled-baseline control-plane metadata through the controltower management
	// APIs (ListLandingZones, GetLandingZone, ListEnabledBaselines,
	// ListEnabledControls, ListTagsForResource) and never reads or persists the
	// landing-zone manifest body, control or baseline parameter values, or any
	// account governance payload, and never enables, disables, resets, or
	// mutates Control Tower state.
	ServiceControlTower = "controltower"
)

const (
	// ResourceTypeControlTowerLandingZone identifies an AWS Control Tower landing
	// zone metadata resource. The scanner emits identity (ARN), deployed version,
	// lifecycle status, and drift status only. The landing-zone manifest JSON
	// body, which carries governance configuration, stays outside the contract.
	ResourceTypeControlTowerLandingZone = "aws_controltower_landing_zone"
	// ResourceTypeControlTowerEnabledControl identifies an AWS Control Tower
	// enabled control metadata resource. The scanner emits identity (the enabled
	// control ARN), the control identifier, the governed organizational-unit
	// target, and deployment and drift status only. Control parameter values stay
	// outside the contract.
	ResourceTypeControlTowerEnabledControl = "aws_controltower_enabled_control"
	// ResourceTypeControlTowerEnabledBaseline identifies an AWS Control Tower
	// enabled baseline metadata resource. The scanner emits identity (the enabled
	// baseline ARN), the baseline identifier and enabled version, the governed
	// target, and deployment and drift status only. Baseline parameter values
	// stay outside the contract.
	ResourceTypeControlTowerEnabledBaseline = "aws_controltower_enabled_baseline"
)

const (
	// RelationshipControlTowerControlGovernsTarget records that an AWS Control
	// Tower enabled control governs an Organizations organizational unit (or, for
	// account-scoped targets, an account). The target is keyed by the bare
	// Organizations id (ou-…, account id, or r-…) parsed from the Control Tower
	// target ARN so the edge joins the node the organizations scanner publishes.
	RelationshipControlTowerControlGovernsTarget = "controltower_control_governs_target"
	// RelationshipControlTowerBaselineGovernsTarget records that an AWS Control
	// Tower enabled baseline governs an Organizations organizational unit, account,
	// or root. The target is keyed by the bare Organizations id parsed from the
	// Control Tower target ARN so the edge joins the organizations scanner node.
	RelationshipControlTowerBaselineGovernsTarget = "controltower_baseline_governs_target"
	// RelationshipControlTowerBaselineForLandingZone records that an AWS Control
	// Tower enabled baseline belongs to a landing zone in the same boundary. The
	// edge is internal to this scanner and is keyed by the landing-zone ARN the
	// landing-zone node publishes.
	RelationshipControlTowerBaselineForLandingZone = "controltower_baseline_for_landing_zone"
)
