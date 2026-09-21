// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK ACM Private CA (acm-pca) control-plane calls
// into the scanner-owned certificate authority metadata the acmpca package
// consumes.
//
// The adapter pages ListCertificateAuthorities, enriches each authority with
// DescribeCertificateAuthority and ListTags, and surfaces only safe metadata.
// The accepted SDK surface is the internal apiClient interface, which
// deliberately excludes IssueCertificate, GetCertificate,
// GetCertificateAuthorityCsr, GetCertificateAuthorityCertificate, and every
// Create/Delete/Update/Restore/Import lifecycle API. A reflective guard test
// fails if any of those methods appears on the interface, so a future SDK
// refactor cannot quietly broaden the contract.
package awssdk
