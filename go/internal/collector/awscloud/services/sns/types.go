package sns

import "context"

// Client is the SNS metadata read surface consumed by Scanner. Runtime
// adapters should translate AWS SDK responses into scanner-owned topic records.
type Client interface {
	ListTopics(context.Context) ([]Topic, error)
}

// Topic is the scanner-owned representation of one SNS topic. It contains
// topic metadata and safe subscription relationship evidence only.
type Topic struct {
	ARN           string
	Name          string
	Tags          map[string]string
	Attributes    TopicAttributes
	Subscriptions []Subscription
}

// TopicAttributes contains safe SNS topic metadata fields. Topic policy JSON,
// delivery policies, data protection policies, and message payloads stay out
// of this contract.
type TopicAttributes struct {
	DisplayName               string
	Owner                     string
	SubscriptionsConfirmed    string
	SubscriptionsDeleted      string
	SubscriptionsPending      string
	SignatureVersion          string
	TracingConfig             string
	KMSMasterKeyID            string
	FIFOTopic                 bool
	ContentBasedDeduplication bool
	ArchivePolicy             string
	BeginningArchiveTime      string
}

// Subscription is safe SNS subscription metadata. Endpoint is populated only
// when AWS reports an ARN-shaped endpoint; email, SMS, HTTP, and HTTPS
// endpoints are intentionally omitted.
type Subscription struct {
	SubscriptionARN string
	Protocol        string
	Owner           string
	EndpointARN     string
}
