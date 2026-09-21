// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Backup responses to the
// scanner-owned backup.Client contract.
//
// The adapter is the only place inside the AWS cloud collector that talks to
// the AWS Backup SDK. It exposes a small, metadata-only read surface:
// ListBackupVaults, ListBackupPlans, ListBackupSelections,
// ListRecoveryPoints, ListReportPlans, ListRestoreTestingPlans, and
// ListFrameworks. The adapter does not expose mutation APIs
// (Create/Update/Delete vault/plan/selection/report plan/restore testing
// plan/framework, StartBackupJob, StartRestoreJob, StartCopyJob,
// DeleteRecoveryPoint, PutBackupVaultAccessPolicy) and does not call
// GetBackupVaultAccessPolicy. Recovery-point restore metadata
// (GetRecoveryPointRestoreMetadata) and recovery-point object contents are
// never read; only identity and timing metadata is projected to the
// scanner-owned record.
package awssdk
