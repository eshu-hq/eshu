// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 ACM calls into scanner-owned
// certificate metadata.
//
// The adapter calls only ListCertificates, DescribeCertificate, and
// ListTagsForCertificate. It MUST NOT call GetCertificate (which returns the
// PEM body) and MUST NOT call ExportCertificate (which returns private key
// material). The internal apiClient interface deliberately excludes both
// methods, and a reflection-based test verifies the exclusion so a future
// SDK refactor cannot quietly add either back.
//
// The adapter also does not call any ACM mutation API (ImportCertificate,
// DeleteCertificate, RenewCertificate, RequestCertificate,
// UpdateCertificateOptions, ResendValidationEmail, RemoveTagsFromCertificate).
// ACM Private CA (acm-pca) is out of scope.
package awssdk
