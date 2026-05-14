package sqs

import "context"

// Client is the SQS read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned metadata records.
type Client interface {
	ListQueues(context.Context) ([]Queue, error)
}

// Queue is the scanner-owned representation of one SQS queue. It contains
// queue metadata only; message bodies and queue policy JSON are intentionally
// outside this contract.
type Queue struct {
	ARN        string
	URL        string
	Name       string
	Tags       map[string]string
	Attributes QueueAttributes
}

// QueueAttributes contains the metadata attributes Eshu persists for one SQS
// queue. Excluded AWS attributes such as Policy stay out because they can carry
// access-policy JSON rather than inventory metadata.
type QueueAttributes struct {
	DelaySeconds                  string
	FIFOQueue                     bool
	ContentBasedDeduplication     bool
	DeduplicationScope            string
	FIFOThroughputLimit           string
	MaximumMessageSize            string
	MessageRetentionPeriod        string
	ReceiveMessageWaitTimeSeconds string
	VisibilityTimeout             string
	KMSMasterKeyID                string
	KMSDataKeyReusePeriodSeconds  string
	SQSManagedSSEEnabled          bool
	DeadLetterTargetARN           string
	MaxReceiveCount               string
	RedrivePermission             string
	RedriveSourceQueueARNs        []string
	CreatedTimestamp              string
	LastModifiedTimestamp         string
}
