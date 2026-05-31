// Package computeoptimizer maps AWS Compute Optimizer recommendation metadata
// into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for recommendation summaries
// (one per resource type) and per-resource recommendations for EC2 instances,
// Auto Scaling groups, EBS volumes, and Lambda functions, plus
// recommendation-to-target relationships to the analyzed EC2 instance (bare
// instance id), Auto Scaling group (group name), and Lambda function (function
// ARN). EBS volume recommendations carry no edge because Eshu does not scan an
// EBS volume resource; the volume identity is recorded as metadata instead of a
// dangling graph edge. The scanner is metadata-only: it never mutates Compute
// Optimizer state, never changes enrollment, and never persists the CloudWatch
// utilization metric data points behind a recommendation.
package computeoptimizer
