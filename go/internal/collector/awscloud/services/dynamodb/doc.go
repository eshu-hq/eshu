// Package dynamodb converts Amazon DynamoDB control-plane metadata into AWS
// cloud collector facts.
//
// The package owns scanner-level table fact selection and direct KMS
// relationship evidence. It does not own AWS SDK pagination, credential
// loading, workflow claims, fact persistence, graph writes, reducer admission,
// workload ownership, or query behavior.
package dynamodb
