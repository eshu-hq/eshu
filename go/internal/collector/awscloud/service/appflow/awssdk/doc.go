// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 AppFlow client into the
// metadata-only AppFlow scanner interface.
//
// The adapter uses ListFlows, DescribeFlow, and DescribeConnectorProfiles. It
// reads only the source/destination connector types, connector profile names,
// S3 bucket references, customer KMS key ARN, trigger type, and the connector
// profile's Secrets Manager credentials ARN. It intentionally never reads the
// DescribeFlow Tasks list (field mappings, which can encode literal data
// values), flow run records (DescribeFlowExecutionRecords), connector
// credentials, or OAuth tokens, and never calls StartFlow, StopFlow, or any
// Create/Update/Delete AppFlow API. The accepted SDK surface excludes those
// operations by construction.
package awssdk
