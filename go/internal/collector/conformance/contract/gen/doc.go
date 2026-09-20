// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command contract-gen reads specs/collector_fact_contract.v1.yaml and
// emits go/internal/collector/conformance/contract/data.go. Pass -check to
// verify the committed output is current without writing.
//
// This command is invoked by scripts/generate-contracttest.sh (write mode)
// and scripts/verify-contracttest.sh (check mode).
package main
