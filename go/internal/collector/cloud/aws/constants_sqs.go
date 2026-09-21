// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSQS identifies the regional Amazon Simple Queue Service metadata
	// scan slice.
	ServiceSQS = "sqs"
)

const (
	// ResourceTypeSQSQueue identifies an SQS queue metadata resource.
	ResourceTypeSQSQueue = "aws_sqs_queue"
)

const (
	// RelationshipSQSQueueUsesDeadLetterQueue records SQS redrive policy
	// evidence from a source queue to its dead-letter queue.
	RelationshipSQSQueueUsesDeadLetterQueue = "sqs_queue_uses_dead_letter_queue"
)
