// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package transfer maps AWS Transfer Family server and service-managed user
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Transfer servers and
// users plus relationships for server-to-VPC-endpoint, server-to-Elastic-IP,
// server-to-ACM-certificate (FTPS), server-to-CloudWatch-log-group,
// server-to-logging-IAM-role, user-to-IAM-role, user-home-directory-to-S3-bucket,
// and user-home-directory-to-EFS-file-system evidence. Host key fingerprints,
// host key material, SSH public key bodies, user policy JSON, POSIX UID/GID
// material, login banners, identity-provider invocation secrets, and mutation
// APIs stay outside this package contract. Home-directory mappings are recorded
// as paths only; object and file contents are never read.
package transfer
