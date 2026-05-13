// Package ecr scans Amazon Elastic Container Registry source truth into AWS
// cloud fact observations.
//
// The package owns scanner-level ECR fact selection for repositories,
// lifecycle policies, and image references. SDK adapters belong in the awssdk
// subpackage so scanner tests can use small fakes instead of AWS SDK clients.
package ecr
