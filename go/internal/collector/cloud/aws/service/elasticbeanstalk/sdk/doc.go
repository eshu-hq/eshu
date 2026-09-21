// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Elastic Beanstalk client to the
// Elastic Beanstalk scanner contract.
//
// The package owns Elastic Beanstalk describe-call pagination, SDK response
// mapping, AWS API telemetry, throttle detection, and pagination spans. The
// accepted SDK surface is metadata-only by construction: it lists no
// application/environment mutation, environment rebuild/terminate, CNAME swap,
// environment-info data-plane, or configuration-validation operation, proven by
// a reflective guard test on the apiClient interface.
package awssdk
