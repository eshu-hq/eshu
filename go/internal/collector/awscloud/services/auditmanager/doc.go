// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auditmanager maps AWS Audit Manager assessment, framework, and control
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for assessments, frameworks,
// and controls plus relationships for assessment-to-framework,
// assessment-to-S3 (the assessment-reports destination bucket),
// assessment-to-KMS-key (the account settings encryption key), and
// assessment-to-account (in-scope accounts). Collected audit evidence, evidence
// finder records, change logs, delegation comments, control narratives
// (testing information, action-plan instructions), and assessment report URLs
// stay outside this package contract: the scanner is metadata-only and never
// mutates Audit Manager state.
package auditmanager
