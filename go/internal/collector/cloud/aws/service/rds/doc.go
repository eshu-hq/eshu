// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rds emits metadata-only Amazon RDS resource, relationship, and
// posture facts for the AWS cloud collector.
//
// The package owns scanner-level fact selection for DB instances, DB clusters,
// and DB subnet groups. For every DB instance and Aurora DB cluster it also
// emits one rds_instance_posture fact carrying derived security and operations
// posture (public exposure, encryption and KMS key, IAM database
// authentication, backup/multi-AZ/deletion-protection, Performance Insights
// configuration, parameter/option-group identity, a curated set of
// security-relevant parameters, and the CA certificate identifier). Posture
// facts are emitted in the same bounded describe pass and add no per-resource
// AWS API fan-out. The package deliberately avoids database connections,
// secrets, snapshots, log contents, Performance Insights samples, schemas,
// tables, row data, and workload ownership inference. It emits no graph edges;
// reducers own posture projection. AWS SDK pagination and API telemetry live in
// the awssdk adapter.
package rds
