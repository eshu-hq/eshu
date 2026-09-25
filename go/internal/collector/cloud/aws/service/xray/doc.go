// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package xray emits AWS X-Ray configuration-only facts for one claimed AWS
// boundary. It covers X-Ray groups (name, ARN, trace filter expression,
// insights flags), sampling rules (name, ARN, priority, reservoir, fixed rate,
// and service match criteria), and the account-region encryption configuration
// (type, status, KMS key reference).
//
// The scanner is configuration-only. It never reads or persists X-Ray
// observability payload — traces, trace summaries, segments, or service-graph
// (service-map) data — and never calls a mutation API. The companion awssdk
// adapter exposes exactly three reads (GetGroups, GetSamplingRules,
// GetEncryptionConfig); a reflection test asserts no trace, service-graph,
// insight, telemetry, or mutation method is reachable.
//
// It emits two relationships: the encryption configuration to its KMS key
// (target_type aws_kms_key, only when encryption is KMS and a key reference is
// reported) and each sampling rule to the service identity it matches by name
// and type (target_type aws_xray_service_correlation, a labeled correlation
// anchor reducers resolve to the real service node by name).
package xray
