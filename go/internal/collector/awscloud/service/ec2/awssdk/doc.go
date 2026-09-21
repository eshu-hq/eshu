// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 EC2 responses to the EC2 scanner
// contract.
//
// It owns paginated read-only calls for network topology, instance posture
// inputs, and boundary-scoped EBS volume metadata. It returns scanner-owned
// records only; AWS SDK types do not cross into the scanner package.
package awssdk
