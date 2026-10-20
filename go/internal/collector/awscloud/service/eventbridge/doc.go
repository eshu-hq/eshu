// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package eventbridge maps Amazon EventBridge metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence event bus and rule resources plus
// relationships for rule membership and ARN-addressable targets. Event bus
// policy JSON, target input payloads, input transformers, HTTP parameters, and
// mutation APIs stay outside this package contract.
package eventbridge
