// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs a credential-free Prometheus/Mimir query-range fixture for
// deployed golden-corpus validation.
//
// The binary accepts one closed queue-depth query over one hour at a five-minute
// step and returns two public synthetic samples. It rejects every other request,
// so a passing proof exercises Eshu's configured range-source wiring without
// turning this test fixture into a general PromQL service.
package main
