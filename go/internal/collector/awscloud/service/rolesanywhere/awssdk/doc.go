// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 IAM Roles Anywhere client into the
// metadata-only Roles Anywhere scanner interface.
//
// The adapter uses ListTrustAnchors, ListProfiles, ListCrls, and
// ListTagsForResource to read trust-anchor, profile, and CRL control-plane
// metadata and resource tags. It intentionally excludes GetCrl (which returns
// the CRL body bytes), GetSubject and ListSubjects (which expose vended session
// credentials), and every Create/Update/Delete/Import/Enable/Disable mutation
// API, so the adapter cannot read certificate private material, CRL bodies, or
// session credentials, and cannot write Roles Anywhere state. It also drops the
// PEM x509 certificate data carried on certificate-bundle trust anchors and the
// inline session policy document carried on profiles.
package awssdk
