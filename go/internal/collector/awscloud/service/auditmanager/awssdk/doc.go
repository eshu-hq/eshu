// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Audit Manager client into the
// metadata-only Audit Manager scanner interface.
//
// The adapter uses GetAccountStatus, ListAssessments, GetAssessment,
// ListAssessmentFrameworks, ListControls, GetSettings, and ListTagsForResource
// to read assessment, framework, and control control-plane metadata plus the
// account-level encryption key. It intentionally excludes GetEvidence and every
// evidence reader, GetEvidenceFolder, GetChangeLogs, GetDelegations,
// GetAssessmentReportUrl, GetControl (the control narrative), the insights
// readers, and all Create/Update/Delete/Register/Batch mutation APIs, so the
// adapter cannot read collected evidence, evidence finder records, change logs,
// delegation comments, control narratives, or report URLs and cannot mutate
// Audit Manager state.
package awssdk
