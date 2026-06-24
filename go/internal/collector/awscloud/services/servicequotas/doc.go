// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicequotas maps AWS Service Quotas applied-quota metadata into AWS
// cloud collector facts.
//
// The scanner emits one reported-confidence resource per applied service quota
// for the claimed account and region. Each quota carries its identity (service
// code, quota code, name, ARN), the applied value, the AWS-published default
// value joined by quota code, an override flag that is true when the applied
// value differs from the default, the adjustable/global flags, the unit, the
// optional rate period, the applied level, the optional resource-level quota
// context, and the CloudWatch usage-metric identity AWS recommends for tracking
// usage. The scanner emits no relationships: a quota references an AWS service
// code, not a scanned resource, so there is no cross-service edge to key without
// dangling the graph. The scanner is metadata-only: it never requests, modifies,
// or deletes a quota and never associates a quota-increase template.
package servicequotas
