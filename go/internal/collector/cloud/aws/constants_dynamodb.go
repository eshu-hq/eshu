// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDynamoDB identifies the regional Amazon DynamoDB metadata scan
	// slice.
	ServiceDynamoDB = "dynamodb"
)

const (
	// ResourceTypeDynamoDBTable identifies a DynamoDB table metadata resource.
	ResourceTypeDynamoDBTable = "aws_dynamodb_table"
)

const (
	// RelationshipDynamoDBTableUsesKMSKey records a DynamoDB table's reported
	// server-side encryption KMS key dependency.
	RelationshipDynamoDBTableUsesKMSKey = "dynamodb_table_uses_kms_key"
)
