// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contracttest provides reusable fact-shape contract test helpers that
// any collector package can import to assert its emitted facts match the
// declared contract in specs/collector_fact_contract.v1.yaml.
//
// The helpers cover three concerns: fact-kind membership (emitted kinds must
// be a subset of declared kinds), required payload keys (every declared
// required key must be present), and malformed-input rejection (nil client
// and service-kind mismatch must produce errors).
package contracttest
