// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSNS identifies the regional Amazon Simple Notification Service
	// metadata scan slice.
	ServiceSNS = "sns"
)

const (
	// ResourceTypeSNSTopic identifies an SNS topic metadata resource.
	ResourceTypeSNSTopic = "aws_sns_topic"
)

const (
	// RelationshipSNSTopicDeliversToResource records SNS subscription
	// evidence from a topic to an ARN-addressable subscriber.
	RelationshipSNSTopicDeliversToResource = "sns_topic_delivers_to_resource"
)
