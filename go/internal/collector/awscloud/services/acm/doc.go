// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package acm maps AWS Certificate Manager certificate metadata into AWS
// cloud collector facts.
//
// The package owns scanner-level certificate normalization only. It never calls
// the AWS SDK directly, never calls GetCertificate (which returns the PEM
// body), never calls ExportCertificate (which returns private key material),
// and never persists certificate body PEM or private key material. SDK adapters
// provide Certificate values, and Scanner emits aws_resource facts plus
// optional certificate-to-using-resource relationship evidence built from
// ACM-reported in-use-by ARNs.
//
// ACM Private CA (the acm-pca service) is out of scope for this package.
package acm
