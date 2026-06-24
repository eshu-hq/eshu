// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pagerduty converts redacted synthetic PagerDuty observations into
// collector SDK result records.
//
// The package is a fixture-only reference component. It emits namespaced
// component facts whose payload and provenance shape mirror the in-tree
// PagerDuty source fact contract, while leaving live provider calls, hosted
// scheduling, reducer admission, graph truth, and API readback to core Eshu.
package pagerduty
