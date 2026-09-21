// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lambda turns AWS Lambda function, alias, and event-source mapping
// observations into AWS collector facts.
//
// The package owns scanner-side Lambda models and fact-envelope selection for
// the AWS cloud collector. It preserves reported runtime, image, environment,
// VPC, alias, and event-source evidence without calling AWS APIs directly or
// materializing canonical graph truth.
package lambda
