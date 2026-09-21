// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package appflow maps Amazon AppFlow flow and connector profile metadata into
// AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for flows (ARN, name, status,
// source/destination connector types, connector profile names, trigger type)
// and connector profiles (ARN, name, connector type, connection mode) plus
// relationships for flow-to-S3-source-bucket, flow-to-S3-destination-bucket,
// flow-to-connector-profile, flow-to-KMS-key, and connector-profile-to-Secrets
// Manager-secret evidence.
//
// The scanner is metadata only. Field mappings (the flow's task transforms,
// which can encode literal data values), flow run records, execution result
// payloads, connector credentials, and OAuth tokens stay outside this package
// contract. The only credential reference recorded is the Secrets Manager
// credentials ARN, used solely to join the connector profile to its secret
// node; the credential values themselves are never read.
package appflow
