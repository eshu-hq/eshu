// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceGuardDuty identifies the regional Amazon GuardDuty metadata scan
	// slice.
	ServiceGuardDuty = "guardduty"
)

const (
	// ResourceTypeGuardDutyDetector identifies a GuardDuty detector metadata
	// resource.
	ResourceTypeGuardDutyDetector = "aws_guardduty_detector"
	// ResourceTypeGuardDutyMemberAccount identifies a GuardDuty member account
	// reported by an administrator detector.
	ResourceTypeGuardDutyMemberAccount = "aws_guardduty_member_account"
	// ResourceTypeGuardDutyFilter identifies a GuardDuty saved filter summary.
	ResourceTypeGuardDutyFilter = "aws_guardduty_filter"
	// ResourceTypeGuardDutyPublishingDestination identifies a GuardDuty finding
	// publishing destination.
	ResourceTypeGuardDutyPublishingDestination = "aws_guardduty_publishing_destination"
	// ResourceTypeGuardDutyThreatIntelSet identifies a GuardDuty threat intel
	// set metadata summary.
	ResourceTypeGuardDutyThreatIntelSet = "aws_guardduty_threat_intel_set"
	// ResourceTypeGuardDutyIPSet identifies a GuardDuty IP set metadata summary.
	ResourceTypeGuardDutyIPSet = "aws_guardduty_ip_set"
)

const (
	// RelationshipGuardDutyDetectorHasMemberAccount records a GuardDuty
	// administrator detector's member account.
	RelationshipGuardDutyDetectorHasMemberAccount = "guardduty_detector_has_member_account"
	// RelationshipGuardDutyDetectorPublishesToDestination records a detector's
	// finding publishing destination.
	RelationshipGuardDutyDetectorPublishesToDestination = "guardduty_detector_publishes_to_destination"
	// RelationshipGuardDutyDetectorUsesThreatIntelSet records a detector's threat
	// intel set membership.
	RelationshipGuardDutyDetectorUsesThreatIntelSet = "guardduty_detector_uses_threat_intel_set"
	// RelationshipGuardDutyDetectorUsesIPSet records a detector's IP set
	// membership.
	RelationshipGuardDutyDetectorUsesIPSet = "guardduty_detector_uses_ip_set"
)
