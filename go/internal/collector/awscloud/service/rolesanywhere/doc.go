// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rolesanywhere maps AWS IAM Roles Anywhere trust anchor, profile, and
// certificate-revocation-list (CRL) metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for trust anchors, profiles,
// and imported CRLs plus relationships for profile-to-IAM-role,
// trust-anchor-to-ACM-PCA (only for AWS_ACM_PCA trust anchors), and
// CRL-to-trust-anchor evidence. Certificate private material, PEM certificate
// bundles, CRL body bytes, inline session policy documents, certificate
// attribute-mapping rules, and vended session credentials stay outside this
// package contract: the scanner is metadata-only.
package rolesanywhere
