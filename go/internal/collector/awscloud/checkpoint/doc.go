// Package checkpoint defines the durable AWS pagination checkpoint contract.
//
// Checkpoints are scoped to one workflow-owned AWS service claim and one
// service operation. They intentionally track the page token that is safe to
// retry, not a promise that later facts have already committed.
package checkpoint
