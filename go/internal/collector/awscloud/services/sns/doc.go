// Package sns maps Amazon SNS topic metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence topic resources and subscription
// relationships only when the subscription endpoint is an ARN. Topic policy
// JSON, delivery-policy JSON, data-protection-policy JSON, raw email or phone
// endpoints, and message payloads stay outside this package contract.
package sns
