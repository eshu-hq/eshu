// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gittfstate emits advisory warnings about Terraform backend
// configuration expressed in repository HCL.
//
// It is metadata-only by design: the git collector never reads or persists raw
// Terraform state bytes, only the shape of the backend declaration.
package gittfstate
