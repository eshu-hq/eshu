// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package acmpca maps AWS Certificate Manager Private CA (acm-pca)
// certificate authority metadata into AWS cloud collector facts.
//
// The package owns scanner-level certificate authority normalization only. It
// never calls the AWS SDK directly, never issues or exports certificates, and
// never reads certificate signing requests, certificate chains, or private key
// material. SDK adapters provide CertificateAuthority values, and Scanner emits
// aws_resource facts keyed by the certificate authority ARN plus optional
// CA-to-KMS-key and subordinate-CA-to-parent-CA relationship evidence.
//
// The certificate authority resource_id is the CA ARN. App Mesh virtual-node
// client TLS trust edges target aws_acmpca_certificate_authority keyed by that
// same ARN, so the resource_id contract closes that dangling edge.
package acmpca
