// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfstatewarning classifies Terraform-state warning facts for operator
// readbacks.
//
// The package exposes a closed mapping from stable warning_kind/reason pairs to
// severity and actionability labels. Collector code uses it while emitting
// warning facts, and status code uses the same table when older persisted rows
// lack explicit classification fields. Unknown pairs remain unclassified so new
// warning shapes do not silently inherit the wrong operational meaning.
package tfstatewarning
